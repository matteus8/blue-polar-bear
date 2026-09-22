package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

func TestGateway_TelemetryAndFleet(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gw.hub.run(ctx)

	telem := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  85.0,
		Coordinates: schema.Coordinates{Latitude: 31.62, Longitude: -8.08, AltitudeM: 100},
		Sequence:    1,
	}
	env, err := schema.NewTelemetryEnvelope(schema.Tier1Public, "cds-sanitized", telem, nil)
	if err != nil {
		t.Fatalf("creating envelope: %v", err)
	}
	envBytes, _ := json.Marshal(env)

	gw.handleTelemetry("sec/tier1/drone/blue/bravo/telemetry", envBytes)

	// Verify fleet state
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet", nil)
	w := httptest.NewRecorder()
	gw.handleFleet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", w.Code)
	}

	var fleet map[string]VehicleState
	if err := json.Unmarshal(w.Body.Bytes(), &fleet); err != nil {
		t.Fatalf("parsing fleet response: %v", err)
	}

	v, exists := fleet["bravo"]
	if !exists {
		t.Fatalf("expected vehicle 'bravo' in fleet")
	}
	if v.Telemetry.BatteryPct != 85.0 {
		t.Errorf("expected battery 85.0, got %f", v.Telemetry.BatteryPct)
	}
	if len(v.TrackPoints) != 1 {
		t.Errorf("expected 1 track point, got %d", len(v.TrackPoints))
	}
}

func TestGateway_DispatchCommand(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081")

	receivedChan := make(chan []byte, 1)
	bus.Subscribe(context.Background(), "sec/tier2/drone/blue/bravo/command", func(key string, payload []byte) {
		receivedChan <- payload
	})

	body := []byte(`{"target_vehicle":"bravo","command_type":"RETURN_TO_BASE","parameters":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader(body))
	w := httptest.NewRecorder()
	gw.handleCommand(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", w.Code, w.Body.String())
	}

	var receivedCommand []byte
	select {
	case receivedCommand = <-receivedChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for command envelope to be published to Zenoh")
	}

	cmdEnv, err := zenohutil.ParseJSONEnvelope(receivedCommand)
	if err != nil {
		t.Fatalf("parsing published command envelope: %v", err)
	}

	if cmdEnv.Command.CommandType != "RETURN_TO_BASE" {
		t.Errorf("expected command RETURN_TO_BASE, got %s", cmdEnv.Command.CommandType)
	}
}

func TestGateway_BackhaulEndpoints(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	spoolDir := t.TempDir()
	spoolFile := filepath.Join(spoolDir, "test-spool.jsonl")

	mule, err := zenohutil.NewDataMule(spoolFile, 50, "relay.platformstaq.com", bus)
	if err != nil {
		t.Fatalf("creating datamule: %v", err)
	}
	defer mule.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081", mule)

	// 1. Check GET /api/v1/backhaul
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backhaul", nil)
	w := httptest.NewRecorder()
	gw.handleBackhaul(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	var metrics zenohutil.BackhaulMetrics
	if err := json.Unmarshal(w.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("parsing backhaul metrics: %v", err)
	}
	if metrics.Status != zenohutil.BackhaulOnline {
		t.Errorf("expected initial status ONLINE, got %s", metrics.Status)
	}

	// 2. Simulate DDIL blackout via POST /api/v1/backhaul/simulate
	simBody := []byte(`{"ddil_active": true}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/backhaul/simulate", bytes.NewReader(simBody))
	w = httptest.NewRecorder()
	gw.handleBackhaulSimulate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("parsing backhaul metrics after sim: %v", err)
	}
	if metrics.Status != zenohutil.BackhaulDDILBuffering {
		t.Errorf("expected DDIL_BUFFERING status, got %s", metrics.Status)
	}

	// 3. Send telemetry during DDIL -> verify spooled
	telem := schema.TelemetryPayload{
		VehicleID:   "alpha",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  90.0,
		Coordinates: schema.Coordinates{Latitude: 31.65, Longitude: -8.01, AltitudeM: 120},
		Sequence:    1,
	}
	env, err := schema.NewTelemetryEnvelope(schema.Tier1Public, "cds-sanitized", telem, nil)
	if err != nil {
		t.Fatalf("creating envelope: %v", err)
	}
	envBytes, _ := json.Marshal(env)
	gw.handleTelemetry("sec/tier1/drone/blue/alpha/telemetry", envBytes)

	m := mule.Metrics()
	if m.SpooledPackets != 1 {
		t.Errorf("expected 1 spooled packet during DDIL, got %d", m.SpooledPackets)
	}

	// 4. Restore satellite link via POST /api/v1/backhaul/simulate
	simBody = []byte(`{"ddil_active": false}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/backhaul/simulate", bytes.NewReader(simBody))
	w = httptest.NewRecorder()
	gw.handleBackhaulSimulate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("parsing backhaul metrics after restore: %v", err)
	}
	if metrics.Status != zenohutil.BackhaulOnline {
		t.Errorf("expected ONLINE status after restore, got %s", metrics.Status)
	}
	if metrics.SpooledPackets != 0 {
		t.Errorf("expected 0 spooled packets after flush, got %d", metrics.SpooledPackets)
	}
	if metrics.SyncedTotal != 1 {
		t.Errorf("expected 1 synced packet after flush, got %d", metrics.SyncedTotal)
	}
}

func TestGateway_InvalidCommands(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081", nil)

	// 1. Invalid method (GET instead of POST)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/command", nil)
	w := httptest.NewRecorder()
	gw.handleCommand(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}

	// 2. Malformed JSON
	req = httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader([]byte("{invalid-json")))
	w = httptest.NewRecorder()
	gw.handleCommand(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}

	// 3. Missing target_vehicle
	req = httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader([]byte(`{"command_type":"HOVER"}`)))
	w = httptest.NewRecorder()
	gw.handleCommand(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing target_vehicle, got %d", w.Code)
	}

	// 4. Missing command_type
	req = httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader([]byte(`{"target_vehicle":"alpha"}`)))
	w = httptest.NewRecorder()
	gw.handleCommand(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing command_type, got %d", w.Code)
	}

	// 5. Backhaul simulate invalid method
	req = httptest.NewRequest(http.MethodGet, "/api/v1/backhaul/simulate", nil)
	w = httptest.NewRecorder()
	gw.handleBackhaulSimulate(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on simulate, got %d", w.Code)
	}
}

func TestGateway_WebSocketStreaming(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go gw.hub.run(ctx)

	server := httptest.NewServer(http.HandlerFunc(gw.handleWS))
	defer server.Close()

	// Connect WebSocket client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dialing websocket: %v", err)
	}
	defer ws.Close()

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Send telemetry
	telem := schema.TelemetryPayload{
		VehicleID:   "delta",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  88.0,
		Coordinates: schema.Coordinates{Latitude: 31.65, Longitude: -8.01, AltitudeM: 110},
		Sequence:    1,
	}
	env, err := schema.NewTelemetryEnvelope(schema.Tier1Public, "cds-sanitized", telem, nil)
	if err != nil {
		t.Fatalf("creating envelope: %v", err)
	}
	envBytes, _ := json.Marshal(env)

	gw.handleTelemetry("sec/tier1/drone/blue/delta/telemetry", envBytes)

	// Read message from websocket
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("reading from websocket: %v", err)
	}

	receivedEnv, err := zenohutil.ParseJSONEnvelope(msg)
	if err != nil {
		t.Fatalf("parsing received WS message: %v", err)
	}
	if receivedEnv.Telemetry.VehicleID != "delta" {
		t.Errorf("expected vehicle delta, got %s", receivedEnv.Telemetry.VehicleID)
	}
}

func TestGateway_TargetVehicleAuthorization(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081", nil)

	// 1. Attempt command to adversary red drone -> must return 403 Forbidden
	body := []byte(`{"target_vehicle":"red-1","command_type":"RETURN_TO_BASE","parameters":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader(body))
	w := httptest.NewRecorder()
	gw.handleCommand(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for red-1 target, got %d", w.Code)
	}

	// 2. Command to valid blue drone -> must succeed (200 OK)
	body = []byte(`{"target_vehicle":"blue-bravo","command_type":"PATROL","parameters":{}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/command", bytes.NewReader(body))
	w = httptest.NewRecorder()
	gw.handleCommand(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for blue-bravo target, got %d", w.Code)
	}
}

func TestGateway_InjectEndpoint(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, "http://127.0.0.1:8081", nil)

	// 1. Invalid method (GET instead of POST)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inject", nil)
	w := httptest.NewRecorder()
	gw.handleInject(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /api/v1/inject, got %d", w.Code)
	}

	// 2. Test TIER-3 Critical injection
	receivedChan := make(chan []byte, 1)
	bus.Subscribe(context.Background(), "sec/tier3/drone/blue/blue-alpha/telemetry", func(key string, payload []byte) {
		receivedChan <- payload
	})

	body := []byte(`{"tier":"TIER-3: CRITICAL","tamper":false,"target_vehicle":"blue-alpha"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inject", bytes.NewReader(body))
	w = httptest.NewRecorder()
	gw.handleInject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for inject, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case payload := <-receivedChan:
		env, err := zenohutil.ParseJSONEnvelope(payload)
		if err != nil {
			t.Fatalf("failed to parse injected envelope: %v", err)
		}
		if env.Header.Classification != schema.Tier3Critical {
			t.Errorf("expected TIER-3 classification, got %s", env.Header.Classification)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for injected TIER-3 telemetry")
	}

	// 3. Test Tampered injection
	tamperChan := make(chan []byte, 1)
	bus.Subscribe(context.Background(), "sec/tier2/drone/blue/blue-echo/telemetry", func(key string, payload []byte) {
		tamperChan <- payload
	})

	body = []byte(`{"tier":"TIER-2: RESTRICTED","tamper":true,"target_vehicle":"blue-echo"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/inject", bytes.NewReader(body))
	w = httptest.NewRecorder()
	gw.handleInject(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for tampered inject, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case payload := <-tamperChan:
		env, err := zenohutil.ParseJSONEnvelope(payload)
		if err != nil {
			t.Fatalf("failed to parse injected envelope: %v", err)
		}
		// Validate should fail because digest is corrupted
		if err := env.Validate(); err == nil {
			t.Errorf("expected envelope validation to fail on tampered digest")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for tampered telemetry")
	}
}

func TestGateway_DLQProxy(t *testing.T) {
	// Mock CDS Guard HTTP server
	mockCDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dlq" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"record_id":"DLQ-1","rejection_reason":"TEST_REASON"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockCDS.Close()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	gw := NewGateway(bus, mockCDS.URL, nil)

	// 1. Invalid method (POST instead of GET)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dlq", nil)
	w := httptest.NewRecorder()
	gw.handleDLQ(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST on /api/v1/dlq, got %d", w.Code)
	}

	// 2. Successful proxy to mock CDS
	req = httptest.NewRequest(http.MethodGet, "/api/v1/dlq", nil)
	w = httptest.NewRecorder()
	gw.handleDLQ(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from DLQ proxy, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "DLQ-1") {
		t.Errorf("expected response to contain DLQ-1, got: %s", w.Body.String())
	}

	// 3. Fallback when CDS guard is unreachable
	gwOffline := NewGateway(bus, "http://127.0.0.1:59999", nil)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/dlq", nil)
	w = httptest.NewRecorder()
	gwOffline.handleDLQ(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK fallback from DLQ proxy, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty array [] on CDS offline, got: %s", w.Body.String())
	}
}

