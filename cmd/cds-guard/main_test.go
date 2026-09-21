package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/policy"
	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

func TestCDSGuard_Pipeline(t *testing.T) {
	tempDir := t.TempDir()
	auditPath := filepath.Join(tempDir, "audit.jsonl")

	dlq, err := policy.NewDeadLetterQueue(100, auditPath)
	if err != nil {
		t.Fatalf("creating DLQ: %v", err)
	}
	defer dlq.Close()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	guard := NewCDSGuard(bus, dlq, 2)
	ctx := context.Background()

	// 1. Test Malformed JSON -> Quarantined
	guard.ProcessPacket(ctx, "sec/tier2/drone/blue/bravo/telemetry", []byte("{not valid json"))
	if dlq.Count() != 1 {
		t.Fatalf("expected 1 DLQ record for malformed JSON, got %d", dlq.Count())
	}

	// 2. Test TIER-3 CRITICAL -> Quarantined (Fail-Closed Egress Barred)
	t3Telem := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  90.0,
		Coordinates: schema.Coordinates{Latitude: 31.6249, Longitude: -8.0819, AltitudeM: 100},
	}
	t3Env, err := schema.NewTelemetryEnvelope(schema.Tier3Critical, "edge-node-01", t3Telem, nil)
	if err != nil {
		t.Fatalf("creating t3: %v", err)
	}
	t3Bytes, _ := json.Marshal(t3Env)
	guard.ProcessPacket(ctx, "sec/tier3/drone/blue/bravo/telemetry", t3Bytes)

	if dlq.Count() != 2 {
		t.Fatalf("expected 2 DLQ records after TIER-3 packet, got %d", dlq.Count())
	}

	// 3. Test TIER-2 RESTRICTED -> Redacted and republished
	egressChan := make(chan []byte, 1)
	bus.Subscribe(ctx, "sec/tier1/drone/blue/bravo/telemetry", func(key string, payload []byte) {
		egressChan <- payload
	})

	t2Telem := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  85.0,
		Coordinates: schema.Coordinates{Latitude: 31.624929, Longitude: -8.081916, AltitudeM: 125.0},
		MissionPayload: map[string]any{
			"payload_secret": "confidential_sensor_data",
		},
	}
	t2Env, err := schema.NewTelemetryEnvelope(schema.Tier2Restricted, "edge-node-01", t2Telem, nil)
	if err != nil {
		t.Fatalf("creating t2: %v", err)
	}
	t2Bytes, _ := json.Marshal(t2Env)
	guard.ProcessPacket(ctx, "sec/tier2/drone/blue/bravo/telemetry", t2Bytes)

	var egressReceived []byte
	select {
	case egressReceived = <-egressChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for sanitized packet to be published to egress topic")
	}

	sanitizedEnv, err := zenohutil.ParseJSONEnvelope(egressReceived)
	if err != nil {
		t.Fatalf("parsing sanitized envelope: %v", err)
	}

	if sanitizedEnv.Header.Classification != schema.Tier1Public {
		t.Errorf("expected classification to be %s, got %s", schema.Tier1Public, sanitizedEnv.Header.Classification)
	}

	if sanitizedEnv.Telemetry.Coordinates.Latitude != 31.62 {
		t.Errorf("expected coarsened lat 31.62, got %f", sanitizedEnv.Telemetry.Coordinates.Latitude)
	}
}

func TestCDSGuard_TamperedAndInvalidPackets(t *testing.T) {
	tempDir := t.TempDir()
	auditPath := filepath.Join(tempDir, "audit.jsonl")

	dlq, err := policy.NewDeadLetterQueue(100, auditPath)
	if err != nil {
		t.Fatalf("creating DLQ: %v", err)
	}
	defer dlq.Close()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	guard := NewCDSGuard(bus, dlq, 2)
	ctx := context.Background()

	// 1. Tamper detection: change battery after envelope is signed
	telem := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  85.0,
		Coordinates: schema.Coordinates{Latitude: 31.62, Longitude: -8.08, AltitudeM: 100},
	}
	env, err := schema.NewTelemetryEnvelope(schema.Tier2Restricted, "edge-node-01", telem, nil)
	if err != nil {
		t.Fatalf("creating envelope: %v", err)
	}
	// Tamper: modify battery without updating digest
	env.Telemetry.BatteryPct = 42.0
	tamperedBytes, _ := json.Marshal(env)

	guard.ProcessPacket(ctx, "sec/tier2/drone/blue/bravo/telemetry", tamperedBytes)
	records := dlq.GetRecords(10)
	if len(records) != 1 {
		t.Fatalf("expected 1 quarantined record, got %d", len(records))
	}
	if records[0].RejectionReason != policy.ReasonDigestMismatch {
		t.Errorf("expected rejection %s, got %s", policy.ReasonDigestMismatch, records[0].RejectionReason)
	}

	// 2. Battery violation: battery > 100%
	telemBadBattery := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  150.0,
		Coordinates: schema.Coordinates{Latitude: 31.62, Longitude: -8.08, AltitudeM: 100},
	}
	envBadBat, _ := schema.NewTelemetryEnvelope(schema.Tier2Restricted, "edge-node-01", telemBadBattery, nil)
	badBatBytes, _ := json.Marshal(envBadBat)
	guard.ProcessPacket(ctx, "sec/tier2/drone/blue/bravo/telemetry", badBatBytes)

	records = dlq.GetRecords(10)
	if len(records) != 2 {
		t.Fatalf("expected 2 quarantined records, got %d", len(records))
	}
	if records[1].RejectionReason != policy.ReasonBatteryViolation {
		t.Errorf("expected %s, got %s", policy.ReasonBatteryViolation, records[1].RejectionReason)
	}

	// 3. Out-of-bounds coordinates: latitude > 90
	telemBadCoords := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  80.0,
		Coordinates: schema.Coordinates{Latitude: 99.0, Longitude: -8.08, AltitudeM: 100},
	}
	envBadCoords, _ := schema.NewTelemetryEnvelope(schema.Tier2Restricted, "edge-node-01", telemBadCoords, nil)
	badCoordsBytes, _ := json.Marshal(envBadCoords)
	guard.ProcessPacket(ctx, "sec/tier2/drone/blue/bravo/telemetry", badCoordsBytes)

	records = dlq.GetRecords(10)
	if len(records) != 3 {
		t.Fatalf("expected 3 quarantined records, got %d", len(records))
	}
	if records[2].RejectionReason != policy.ReasonInvalidCoordinates {
		t.Errorf("expected %s, got %s", policy.ReasonInvalidCoordinates, records[2].RejectionReason)
	}
}

func TestCDSGuard_HTTPEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	auditPath := filepath.Join(tempDir, "audit.jsonl")

	dlq, err := policy.NewDeadLetterQueue(100, auditPath)
	if err != nil {
		t.Fatalf("creating DLQ: %v", err)
	}
	defer dlq.Close()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	guard := NewCDSGuard(bus, dlq, 2)

	// Quarantine one packet to populate DLQ
	guard.ProcessPacket(context.Background(), "sec/tier2/bad/packet", []byte("invalid-json"))

	// Test GET /health
	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "HEALTHY",
			"stats":  guard.stats,
		})
	}).ServeHTTP(healthRec, healthReq)

	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", healthRec.Code)
	}

	var healthResp map[string]any
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("parsing health response: %v", err)
	}
	if healthResp["status"] != "HEALTHY" {
		t.Errorf("expected status HEALTHY, got %v", healthResp["status"])
	}

	// Test GET /dlq
	dlqReq := httptest.NewRequest(http.MethodGet, "/dlq", nil)
	dlqRec := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		records := dlq.GetRecords(50)
		_ = json.NewEncoder(w).Encode(records)
	}).ServeHTTP(dlqRec, dlqReq)

	if dlqRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", dlqRec.Code)
	}

	var dlqRecords []policy.DLQRecord
	if err := json.Unmarshal(dlqRec.Body.Bytes(), &dlqRecords); err != nil {
		t.Fatalf("parsing DLQ response: %v", err)
	}
	if len(dlqRecords) != 1 {
		t.Errorf("expected 1 DLQ record, got %d", len(dlqRecords))
	}
}
