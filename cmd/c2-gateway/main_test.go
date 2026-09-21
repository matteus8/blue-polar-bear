package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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
