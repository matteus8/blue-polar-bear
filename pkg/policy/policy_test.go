package policy

import (
	"path/filepath"
	"testing"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

func TestPolicyEngine_EvaluateAndRedact(t *testing.T) {
	pe := NewPolicyEngine(2) // 2 decimal precision

	// 1. Test TIER-1: PUBLIC (Pass-through)
	telem1 := schema.TelemetryPayload{
		VehicleID:   "alpha",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  95.0,
		Coordinates: schema.Coordinates{Latitude: 37.774929, Longitude: -122.419416, AltitudeM: 100},
		Sequence:    1,
	}
	env1, err := schema.NewTelemetryEnvelope(schema.Tier1Public, "edge-node-01", telem1, nil)
	if err != nil {
		t.Fatalf("creating env1: %v", err)
	}

	action, reason, err := pe.Evaluate(env1)
	if err != nil || action != ActionPass {
		t.Fatalf("expected TIER-1 to PASS, got action=%v reason=%v err=%v", action, reason, err)
	}

	// 2. Test TIER-2: RESTRICTED (Redact and Pass)
	telem2 := schema.TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "PATROL",
		BatteryPct:  88.5,
		Coordinates: schema.Coordinates{Latitude: 37.774929, Longitude: -122.419416, AltitudeM: 145.0},
		MissionPayload: map[string]any{
			"target_identified": "high_value_asset",
			"rf_frequency_mhz":  915.5,
		},
		Sequence: 2,
	}
	env2, err := schema.NewTelemetryEnvelope(schema.Tier2Restricted, "edge-node-01", telem2, nil)
	if err != nil {
		t.Fatalf("creating env2: %v", err)
	}

	action, _, err = pe.Evaluate(env2)
	if err != nil || action != ActionRedactPass {
		t.Fatalf("expected TIER-2 to REDACT_AND_PASS, got action=%v err=%v", action, err)
	}

	sanitized, err := pe.RedactAndSanitize(env2)
	if err != nil {
		t.Fatalf("redacting env2: %v", err)
	}

	// Check that classification was down-tagged to TIER-1: PUBLIC
	if sanitized.Header.Classification != schema.Tier1Public {
		t.Errorf("expected classification to be down-tagged to %s, got %s", schema.Tier1Public, sanitized.Header.Classification)
	}

	// Check that GPS coordinates were coarsened
	if sanitized.Telemetry.Coordinates.Latitude != 37.77 {
		t.Errorf("expected lat 37.77, got %f", sanitized.Telemetry.Coordinates.Latitude)
	}
	if sanitized.Telemetry.Coordinates.Longitude != -122.42 {
		t.Errorf("expected lon -122.42, got %f", sanitized.Telemetry.Coordinates.Longitude)
	}

	// Check that mission payload was scrubbed
	if status, ok := sanitized.Telemetry.MissionPayload["status"].(string); !ok || status != "[REDACTED_BY_CDS]" {
		t.Errorf("expected mission payload to be redacted, got: %v", sanitized.Telemetry.MissionPayload)
	}

	// Check that sanitized envelope passes validation with new digest
	if err := sanitized.Validate(); err != nil {
		t.Errorf("sanitized envelope failed validation: %v", err)
	}

	// 3. Test TIER-3: CRITICAL (Fail-Closed Quarantine)
	telem3 := schema.TelemetryPayload{
		VehicleID:   "charlie",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  72.0,
		Coordinates: schema.Coordinates{Latitude: 37.77, Longitude: -122.42, AltitudeM: 200},
		Sequence:    3,
	}
	env3, err := schema.NewTelemetryEnvelope(schema.Tier3Critical, "edge-node-01", telem3, nil)
	if err != nil {
		t.Fatalf("creating env3: %v", err)
	}

	action, reason, _ = pe.Evaluate(env3)
	if action != ActionQuarantine || reason != ReasonTier3EgressProhibited {
		t.Fatalf("expected TIER-3 to be QUARANTINED with ReasonTier3EgressProhibited, got action=%v reason=%v", action, reason)
	}
}

func TestDeadLetterQueue(t *testing.T) {
	tempDir := t.TempDir()
	auditPath := filepath.Join(tempDir, "audit.jsonl")

	dlq, err := NewDeadLetterQueue(10, auditPath)
	if err != nil {
		t.Fatalf("creating DLQ: %v", err)
	}
	defer dlq.Close()

	rec := dlq.Quarantine("sec/tier3/drone/blue/bravo/telemetry", ReasonTier3EgressProhibited, "Barred from egress", "edge-01", schema.Tier3Critical, `{"raw":"data"}`)
	if dlq.Count() != 1 {
		t.Errorf("expected count 1, got %d", dlq.Count())
	}
	if rec.Classification != schema.Tier3Critical {
		t.Errorf("expected %s, got %s", schema.Tier3Critical, rec.Classification)
	}

	records := dlq.GetRecords(5)
	if len(records) != 1 {
		t.Errorf("expected 1 record retrieved, got %d", len(records))
	}
}
