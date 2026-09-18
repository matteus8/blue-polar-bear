package main

import (
	"context"
	"encoding/json"
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
