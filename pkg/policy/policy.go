package policy

import (
	"errors"
	"fmt"
	"math"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

// Policy Action results
type EnforcementAction string

const (
	ActionPass       EnforcementAction = "PASS"
	ActionRedactPass EnforcementAction = "REDACT_AND_PASS"
	ActionQuarantine EnforcementAction = "QUARANTINE"
)

// CDSRejectionReason enumerates why a packet was denied egress.
type CDSRejectionReason string

const (
	ReasonMalformedJSON          CDSRejectionReason = "MALFORMED_JSON_PAYLOAD"
	ReasonSchemaViolation        CDSRejectionReason = "SCHEMA_VALIDATION_FAILURE"
	ReasonDigestMismatch         CDSRejectionReason = "CRYPTOGRAPHIC_INTEGRITY_COMPROMISED"
	ReasonUnauthorizedTier       CDSRejectionReason = "UNAUTHORIZED_SYNTHETIC_TIER"
	ReasonTier3EgressProhibited  CDSRejectionReason = "TIER3_CRITICAL_EGRESS_PROHIBITED"
	ReasonInvalidCoordinates     CDSRejectionReason = "COORDINATES_OUT_OF_BOUNDS"
	ReasonBatteryViolation       CDSRejectionReason = "INVALID_BATTERY_LEVEL"
	ReasonMissingSecurityHeader  CDSRejectionReason = "MISSING_SECURITY_HEADER"
	ReasonUnknownPolicyViolation CDSRejectionReason = "POLICY_VIOLATION_UNKNOWN"
)

// PolicyEngine evaluates packets against cross-domain boundary rules.
type PolicyEngine struct {
	GPSCoarsenDecimals int
}

// NewPolicyEngine initializes a policy engine with specified GPS coarsening precision.
// Typically 2 decimals (~1.1 km) or 3 decimals (~110 m) for redacted egress.
func NewPolicyEngine(gpsDecimals int) *PolicyEngine {
	if gpsDecimals <= 0 {
		gpsDecimals = 2
	}
	return &PolicyEngine{
		GPSCoarsenDecimals: gpsDecimals,
	}
}

// RoundFloat rounds a float to the given number of decimal places.
func RoundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// Evaluate performs zero-trust validation and returns the required enforcement action.
func (pe *PolicyEngine) Evaluate(env *schema.SecurityEnvelope) (EnforcementAction, CDSRejectionReason, error) {
	if env == nil {
		return ActionQuarantine, ReasonSchemaViolation, errors.New("nil envelope")
	}

	// 1. Strict Schema & Cryptographic Integrity Check
	if err := env.Validate(); err != nil {
		return ActionQuarantine, ReasonDigestMismatch, fmt.Errorf("integrity check failed: %w", err)
	}

	// 2. Enforce Synthetic Tier Egress Rules
	switch env.Header.Classification {
	case schema.Tier1Public:
		// TIER-1 is unclassified public data, allowed to egress tactical boundary
		return ActionPass, "", nil

	case schema.Tier2Restricted:
		// TIER-2 requires coarsening and sanitization before egress
		return ActionRedactPass, "", nil

	case schema.Tier3Critical:
		// TIER-3 is sovereign mission data. FAIL-CLOSED: Strictly barred from cloud egress!
		return ActionQuarantine, ReasonTier3EgressProhibited, errors.New("TIER-3: CRITICAL payload cannot egress tactical boundary")

	default:
		return ActionQuarantine, ReasonUnauthorizedTier, fmt.Errorf("unrecognized classification tier: %s", env.Header.Classification)
	}
}

// RedactAndSanitize down-tags and sanitizes a TIER-2 envelope into an egressable TIER-1 envelope.
func (pe *PolicyEngine) RedactAndSanitize(env *schema.SecurityEnvelope) (*schema.SecurityEnvelope, error) {
	if env == nil || env.Telemetry == nil {
		return nil, errors.New("cannot sanitize nil or non-telemetry envelope")
	}

	// Create deep copy of telemetry payload
	sanitizedTelem := *env.Telemetry

	// 1. Coarsen GPS coordinates
	sanitizedTelem.Coordinates.Latitude = RoundFloat(sanitizedTelem.Coordinates.Latitude, pe.GPSCoarsenDecimals)
	sanitizedTelem.Coordinates.Longitude = RoundFloat(sanitizedTelem.Coordinates.Longitude, pe.GPSCoarsenDecimals)
	// Mask exact altitude into 50m quantized increments
	sanitizedTelem.Coordinates.AltitudeM = math.Floor(sanitizedTelem.Coordinates.AltitudeM/50.0) * 50.0

	// 2. Scrub sensitive mission payload
	if sanitizedTelem.MissionPayload != nil {
		sanitizedTelem.MissionPayload = map[string]any{
			"status":    "[REDACTED_BY_CDS]",
			"sanitized": true,
		}
	}

	// 3. Down-tag classification to TIER-1: PUBLIC
	caveats := append([]string{}, env.Header.Caveats...)
	caveats = append(caveats, "REDACTED_FROM_TIER2", "SANITIZED_BY_CDS")

	// 4. Recompute cryptographic digest for sanitized payload
	newDigest, err := schema.ComputePayloadDigest(sanitizedTelem)
	if err != nil {
		return nil, fmt.Errorf("recomputing digest after sanitization: %w", err)
	}

	sanitizedEnv := &schema.SecurityEnvelope{
		Header: schema.SecurityHeader{
			Classification: schema.Tier1Public,
			OriginEnclave:  env.Header.OriginEnclave + "-CDS-SANITIZED",
			TimestampNs:    env.Header.TimestampNs,
			MessageID:      env.Header.MessageID + "-sanitized",
			Digest:         newDigest,
			Caveats:        caveats,
		},
		Telemetry: &sanitizedTelem,
	}

	return sanitizedEnv, nil
}
