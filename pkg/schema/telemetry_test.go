package schema

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSecurityEnvelope_Validate(t *testing.T) {
	telem := TelemetryPayload{
		VehicleID:   "bravo",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  88.5,
		Coordinates: Coordinates{
			Latitude:  34.0522,
			Longitude: -118.2437,
			AltitudeM: 120.0,
		},
		Velocity: Velocity{
			SpeedMps: 15.2,
		},
		HeadingDeg: 180.0,
		Sequence:   1,
	}

	env, err := NewTelemetryEnvelope(Tier2Restricted, "edge-node-01", telem, nil)
	if err != nil {
		t.Fatalf("unexpected error creating envelope: %v", err)
	}

	// Should pass initial validation
	if err := env.Validate(); err != nil {
		t.Fatalf("expected valid envelope, got: %v", err)
	}

	// Test Tamper detection (Digest mismatch)
	env.Telemetry.BatteryPct = 50.0 // Tampered without recomputing digest!
	if err := env.Validate(); err == nil {
		t.Fatalf("expected validation failure due to tampered payload digest mismatch")
	}

	// Test Invalid Classification Tier
	env.Header.Classification = "TOP_SECRET_INVALID"
	if err := env.Validate(); err == nil {
		t.Fatalf("expected validation failure due to unauthorized classification tier")
	}
}

func TestSecurityEnvelope_JSONRoundTrip(t *testing.T) {
	telem := TelemetryPayload{
		VehicleID:   "blue-delta",
		VehicleType: "drone",
		Team:        "blue",
		State:       "AIRBORNE",
		BatteryPct:  98.5,
		Coordinates: Coordinates{
			Latitude:  31.6380,
			Longitude: -8.0950,
			AltitudeM: 145.0,
		},
		Velocity: Velocity{
			SpeedMps: 15.2,
			VX:       10.5,
			VY:       11.2,
		},
		HeadingDeg: 94.58,
		MissionPayload: map[string]any{
			"payload_mode":      "TACTICAL_RECON",
			"target_tracking":   true,
			"ew_emitter_active": false,
			"sensor_temp_c":     38.5,
		},
		Sequence: 1,
	}
	env, err := NewTelemetryEnvelope(Tier2Restricted, "edge-node-01", telem, nil)
	if err != nil {
		t.Fatalf("creating envelope: %v", err)
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling envelope: %v", err)
	}

	var env2 SecurityEnvelope
	if err := json.Unmarshal(data, &env2); err != nil {
		t.Fatalf("unmarshaling envelope: %v", err)
	}

	if err := env2.Validate(); err != nil {
		t.Fatalf("validation failed after JSON roundtrip: %v", err)
	}
}

func TestCommandEnvelope_Validate(t *testing.T) {
	cmd := CommandPayload{
		CommandID:     "cmd-101",
		TargetVehicle: "bravo",
		CommandType:   "RETURN_TO_BASE",
		IssuedAtNs:    time.Now().UnixNano(),
		Issuer:        "operator-1",
	}

	env, err := NewCommandEnvelope(Tier2Restricted, "tactical-gcs-01", cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error creating command envelope: %v", err)
	}

	if err := env.Validate(); err != nil {
		t.Fatalf("expected valid command envelope, got: %v", err)
	}
}
