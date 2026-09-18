package zenohutil

import (
	"fmt"
	"strings"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

// TopicInfo represents the parsed elements of the structured Zenoh key expression.
type TopicInfo struct {
	TierKey     string // e.g. "tier1", "tier2", "tier3"
	VehicleType string // e.g. "drone", "rover", "usv"
	Team        string // e.g. "blue", "red"
	UnitID      string // e.g. "bravo"
	StreamType  string // e.g. "telemetry", "command", "status"
}

// TierToKey maps canonical classification to topic key token.
var TierToKey = map[string]string{
	schema.Tier1Public:     "tier1",
	schema.Tier2Restricted: "tier2",
	schema.Tier3Critical:   "tier3",
}

// KeyToTier maps topic key token back to canonical classification.
var KeyToTier = map[string]string{
	"tier1": schema.Tier1Public,
	"tier2": schema.Tier2Restricted,
	"tier3": schema.Tier3Critical,
}

// BuildKey constructs a standard key expression: sec/<tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>
func BuildKey(tierKey, vehicleType, team, unitID, streamType string) string {
	return fmt.Sprintf("sec/%s/%s/%s/%s/%s",
		strings.ToLower(tierKey),
		strings.ToLower(vehicleType),
		strings.ToLower(team),
		strings.ToLower(unitID),
		strings.ToLower(streamType),
	)
}

// BuildTelemetryKey constructs the telemetry key expression for a given classification and vehicle metadata.
func BuildTelemetryKey(classification, vehicleType, team, unitID string) (string, error) {
	tierKey, ok := TierToKey[classification]
	if !ok {
		return "", fmt.Errorf("unknown classification for topic generation: %s", classification)
	}
	return BuildKey(tierKey, vehicleType, team, unitID, "telemetry"), nil
}

// ParseKey extracts components from a compliant key expression.
func ParseKey(key string) (*TopicInfo, error) {
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if len(parts) != 6 || parts[0] != "sec" {
		return nil, fmt.Errorf("invalid topic key format %q: expected sec/<tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>", key)
	}

	return &TopicInfo{
		TierKey:     parts[1],
		VehicleType: parts[2],
		Team:        parts[3],
		UnitID:      parts[4],
		StreamType:  parts[5],
	}, nil
}
