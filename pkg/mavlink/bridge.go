package mavlink

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

// VehicleState maintains the aggregated autopilot state from incoming MAVLink streams.
type VehicleState struct {
	mu          sync.RWMutex
	unitID      string
	team        string
	vehicleType string
	tier        string
	systemID    byte
	armed       bool
	status      string
	lat         float64
	lon         float64
	alt         float64
	vx          float64
	vy          float64
	vz          float64
	speed       float64
	heading     float64
	battery     float64
	roll        float64
	pitch       float64
	yaw         float64
	lastUpdate  time.Time
	sequence    uint64
}

// NewVehicleState initializes vehicle telemetry aggregation for a given system ID and callsign.
func NewVehicleState(unitID, team, vehicleType, tier string, systemID byte) *VehicleState {
	if tier == "" {
		tier = schema.Tier2Restricted
	}
	if team == "" {
		team = "blue"
	}
	if vehicleType == "" {
		vehicleType = "drone"
	}
	return &VehicleState{
		unitID:      unitID,
		team:        team,
		vehicleType: vehicleType,
		tier:        tier,
		systemID:    systemID,
		battery:     100.0,
		status:      "STANDBY",
	}
}

// IngestFrame updates vehicle state according to the decoded MAVLink frame.
func (v *VehicleState) IngestFrame(frame *Frame) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.lastUpdate = time.Now().UTC()

	switch frame.MessageID {
	case MsgIDHeartbeat:
		hb, err := DecodeHeartbeat(frame.Payload)
		if err != nil {
			return fmt.Errorf("decoding heartbeat: %w", err)
		}
		v.armed = hb.IsArmed()
		if v.armed {
			if v.alt > 1.0 {
				v.status = "IN_FLIGHT"
			} else {
				v.status = "ARMED"
			}
		} else {
			v.status = "STANDBY"
		}

	case MsgIDGlobalPositionInt:
		pos, err := DecodeGlobalPositionInt(frame.Payload)
		if err != nil {
			return fmt.Errorf("decoding global_position_int: %w", err)
		}
		v.lat = roundFloat(pos.LatDegrees(), 6)
		v.lon = roundFloat(pos.LonDegrees(), 6)
		v.alt = roundFloat(pos.AltMeters(), 2)
		v.vx = roundFloat(float64(pos.Vx)/100.0, 2)
		v.vy = roundFloat(float64(pos.Vy)/100.0, 2)
		v.vz = roundFloat(float64(pos.Vz)/100.0, 2)
		v.speed = roundFloat(pos.GroundSpeedMps(), 2)
		v.heading = roundFloat(pos.HeadingDeg(), 1)
		if v.armed && v.alt > 1.0 {
			v.status = "IN_FLIGHT"
		}

	case MsgIDSysStatus:
		sys, err := DecodeSysStatus(frame.Payload)
		if err != nil {
			return fmt.Errorf("decoding sys_status: %w", err)
		}
		if sys.BatteryRemaining >= 0 && sys.BatteryRemaining <= 100 {
			v.battery = float64(sys.BatteryRemaining)
		}

	case MsgIDAttitude:
		att, err := DecodeAttitude(frame.Payload)
		if err != nil {
			return fmt.Errorf("decoding attitude: %w", err)
		}
		v.roll = roundFloat(float64(att.Roll)*180.0/math.Pi, 1)
		v.pitch = roundFloat(float64(att.Pitch)*180.0/math.Pi, 1)
		v.yaw = roundFloat(float64(att.Yaw)*180.0/math.Pi, 1)
	}

	return nil
}

// BuildSecurityEnvelope creates a signed canonical SecurityEnvelope from the current state.
func (v *VehicleState) BuildSecurityEnvelope() (*schema.SecurityEnvelope, error) {
	v.mu.Lock()
	v.sequence++
	seq := v.sequence
	unitID := v.unitID
	team := v.team
	vehicleType := v.vehicleType
	tier := v.tier
	status := v.status
	lat := v.lat
	lon := v.lon
	alt := v.alt
	vx := v.vx
	vy := v.vy
	vz := v.vz
	speed := v.speed
	heading := v.heading
	battery := v.battery
	roll := v.roll
	pitch := v.pitch
	yaw := v.yaw
	v.mu.Unlock()

	payload := schema.TelemetryPayload{
		VehicleID:   unitID,
		VehicleType: vehicleType,
		Team:        team,
		State:       status,
		BatteryPct:  battery,
		Coordinates: schema.Coordinates{
			Latitude:  lat,
			Longitude: lon,
			AltitudeM: alt,
		},
		Velocity: schema.Velocity{
			VX:       vx,
			VY:       vy,
			VZ:       vz,
			SpeedMps: speed,
		},
		HeadingDeg: heading,
		MissionPayload: map[string]any{
			"mavlink_source": true,
			"roll_deg":       roll,
			"pitch_deg":      pitch,
			"yaw_deg":        yaw,
		},
		Sequence: seq,
	}

	origin := fmt.Sprintf("mavlink-%s", unitID)
	return schema.NewTelemetryEnvelope(tier, origin, payload, nil)
}

// GetStateSnapshot returns a copy of key state fields.
func (v *VehicleState) GetStateSnapshot() (lat, lon, alt, speed, heading, battery float64, armed bool, status string) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.lat, v.lon, v.alt, v.speed, v.heading, v.battery, v.armed, v.status
}

func roundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
