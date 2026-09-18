package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Synthetic Classification Constants.
// NEVER use real-world USG/DoD classification strings.
const (
	Tier1Public     = "TIER-1: PUBLIC"
	Tier2Restricted = "TIER-2: RESTRICTED"
	Tier3Critical   = "TIER-3: CRITICAL"
)

// ValidTiers maps all authorized synthetic tiers.
var ValidTiers = map[string]bool{
	Tier1Public:     true,
	Tier2Restricted: true,
	Tier3Critical:   true,
}

// SecurityHeader represents the mandatory zero-trust metadata attached to all packets.
type SecurityHeader struct {
	Classification string   `json:"classification"`
	OriginEnclave  string   `json:"origin_enclave"`
	TimestampNs    int64    `json:"timestamp_ns"`
	MessageID      string   `json:"message_id"`
	Digest         string   `json:"digest"`
	Caveats        []string `json:"caveats,omitempty"`
}

// Coordinates represents spatial positioning of a vehicle.
type Coordinates struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
	AltitudeM float64 `json:"alt_m"`
}

// Velocity represents directional speeds.
type Velocity struct {
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	VZ       float64 `json:"vz"`
	SpeedMps float64 `json:"speed_mps"`
}

// TelemetryPayload represents high-fidelity vehicle telemetry.
type TelemetryPayload struct {
	VehicleID      string         `json:"vehicle_id"`
	VehicleType    string         `json:"vehicle_type"`
	Team           string         `json:"team"`
	State          string         `json:"state"`
	BatteryPct     float64        `json:"battery_pct"`
	Coordinates    Coordinates    `json:"coordinates"`
	Velocity       Velocity       `json:"velocity"`
	HeadingDeg     float64        `json:"heading_deg"`
	MissionPayload map[string]any `json:"mission_payload,omitempty"`
	Sequence       uint64         `json:"sequence"`
}

// CommandPayload represents a C2 instruction sent to an edge vehicle.
type CommandPayload struct {
	CommandID     string         `json:"command_id"`
	TargetVehicle string         `json:"target_vehicle"`
	CommandType   string         `json:"command_type"`
	Parameters    map[string]any `json:"parameters,omitempty"`
	IssuedAtNs    int64          `json:"issued_at_ns"`
	Issuer        string         `json:"issuer"`
}

// SecurityEnvelope represents the unified transmission unit across the Zenoh mesh.
type SecurityEnvelope struct {
	Header    SecurityHeader    `json:"header"`
	Telemetry *TelemetryPayload `json:"telemetry,omitempty"`
	Command   *CommandPayload   `json:"command,omitempty"`
}

// ComputePayloadDigest calculates the SHA-256 digest of the data payload.
func ComputePayloadDigest(payload any) (string, error) {
	if payload == nil {
		return "", errors.New("cannot compute digest for nil payload")
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshalling payload for digest: %w", err)
	}
	hasher := sha256.New()
	hasher.Write(bytes)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// NewTelemetryEnvelope constructs a validated SecurityEnvelope with an updated cryptographic digest.
func NewTelemetryEnvelope(classification, origin string, telemetry TelemetryPayload, caveats []string) (*SecurityEnvelope, error) {
	if !ValidTiers[classification] {
		return nil, fmt.Errorf("invalid synthetic classification tier: %q", classification)
	}

	digest, err := ComputePayloadDigest(telemetry)
	if err != nil {
		return nil, fmt.Errorf("computing telemetry digest: %w", err)
	}

	msgID := fmt.Sprintf("msg-%d-%s", time.Now().UnixNano(), telemetry.VehicleID)
	env := &SecurityEnvelope{
		Header: SecurityHeader{
			Classification: classification,
			OriginEnclave:  origin,
			TimestampNs:    time.Now().UnixNano(),
			MessageID:      msgID,
			Digest:         digest,
			Caveats:        caveats,
		},
		Telemetry: &telemetry,
	}

	return env, nil
}

// NewCommandEnvelope constructs a validated C2 command envelope with cryptographic digest.
func NewCommandEnvelope(classification, origin string, cmd CommandPayload, caveats []string) (*SecurityEnvelope, error) {
	if !ValidTiers[classification] {
		return nil, fmt.Errorf("invalid synthetic classification tier: %q", classification)
	}

	digest, err := ComputePayloadDigest(cmd)
	if err != nil {
		return nil, fmt.Errorf("computing command digest: %w", err)
	}

	if cmd.IssuedAtNs == 0 {
		cmd.IssuedAtNs = time.Now().UnixNano()
	}

	env := &SecurityEnvelope{
		Header: SecurityHeader{
			Classification: classification,
			OriginEnclave:  origin,
			TimestampNs:    time.Now().UnixNano(),
			MessageID:      cmd.CommandID,
			Digest:         digest,
			Caveats:        caveats,
		},
		Command: &cmd,
	}

	return env, nil
}

// Validate performs fail-closed structural, cryptographic, and schema checks.
func (env *SecurityEnvelope) Validate() error {
	if env == nil {
		return errors.New("security envelope is nil")
	}

	// 1. Classification check
	if env.Header.Classification == "" {
		return errors.New("security header missing classification")
	}
	if !ValidTiers[env.Header.Classification] {
		return fmt.Errorf("unauthorized classification tier: %q", env.Header.Classification)
	}

	// 2. Header metadata check
	if env.Header.OriginEnclave == "" {
		return errors.New("security header missing origin enclave")
	}
	if env.Header.TimestampNs == 0 {
		return errors.New("security header missing timestamp_ns")
	}
	if env.Header.Digest == "" {
		return errors.New("security header missing cryptographic digest")
	}

	// 3. Payload payload existence check
	if env.Telemetry == nil && env.Command == nil {
		return errors.New("security envelope contains neither telemetry nor command payload")
	}

	// 4. Digest validation
	var expectedDigest string
	var err error
	if env.Telemetry != nil {
		if env.Telemetry.VehicleID == "" {
			return errors.New("telemetry missing vehicle_id")
		}
		if env.Telemetry.VehicleType == "" {
			return errors.New("telemetry missing vehicle_type")
		}
		if env.Telemetry.Team == "" {
			return errors.New("telemetry missing team")
		}
		if env.Telemetry.BatteryPct < 0.0 || env.Telemetry.BatteryPct > 100.0 {
			return fmt.Errorf("invalid battery percentage: %.2f", env.Telemetry.BatteryPct)
		}
		expectedDigest, err = ComputePayloadDigest(env.Telemetry)
	} else {
		if env.Command.CommandID == "" {
			return errors.New("command missing command_id")
		}
		if env.Command.TargetVehicle == "" {
			return errors.New("command missing target_vehicle")
		}
		if env.Command.CommandType == "" {
			return errors.New("command missing command_type")
		}
		expectedDigest, err = ComputePayloadDigest(env.Command)
	}

	if err != nil {
		return fmt.Errorf("recomputing digest for validation: %w", err)
	}

	if !strings.EqualFold(env.Header.Digest, expectedDigest) {
		return fmt.Errorf("cryptographic digest mismatch: header has %s, computed %s", env.Header.Digest, expectedDigest)
	}

	return nil
}
