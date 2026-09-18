package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
