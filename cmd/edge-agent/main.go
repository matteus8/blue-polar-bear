package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

// VehicleSim encapsulates simulated flight dynamics and vehicle state.
type VehicleSim struct {
	mu           sync.RWMutex
	id           string
	vehicleType  string
	team         string
	state        string
	batteryPct   float64
	centerLat    float64
	centerLon    float64
	currentAlt   float64
	speedMps     float64
	headingDeg   float64
	flightAngle  float64
	orbitRadius  float64
	sequence     uint64
	originNode   string
	ewActive     bool // Simulated electronic warfare / sovereign payload
}

func NewVehicleSim(id, vType, team string, lat, lon float64) *VehicleSim {
	return &VehicleSim{
		id:          id,
		vehicleType: vType,
		team:        team,
		state:       "AIRBORNE",
		batteryPct:  98.5,
		centerLat:   lat,
		centerLon:   lon,
		currentAlt:  120.0,
		speedMps:    14.5,
		headingDeg:  0.0,
		flightAngle: 0.0,
		orbitRadius: 0.008, // ~800 meters orbit
		originNode:  "edge-node-01-pi5",
	}
}

// Step advances the vehicle physics simulation by dt seconds.
func (v *VehicleSim) Step(dt float64) schema.TelemetryPayload {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.sequence++

	// State-dependent behavior
	switch v.state {
	case "AIRBORNE", "PATROL":
		v.flightAngle += 0.08 * dt
		if v.flightAngle > 2*math.Pi {
			v.flightAngle -= 2 * math.Pi
		}
		// Calculate circular orbit around center
		lat := v.centerLat + v.orbitRadius*math.Sin(v.flightAngle)
		lon := v.centerLon + v.orbitRadius*math.Cos(v.flightAngle)
		// Heading tangent to orbit
		v.headingDeg = math.Mod((v.flightAngle*180/math.Pi)+90, 360)
		// Battery consumption
		v.batteryPct = math.Max(5.0, v.batteryPct-(0.05*dt))

		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       v.state,
			BatteryPct:  math.Round(v.batteryPct*10) / 10,
			Coordinates: schema.Coordinates{
				Latitude:  lat,
				Longitude: lon,
				AltitudeM: v.currentAlt + 5.0*math.Sin(v.flightAngle*2),
			},
			Velocity: schema.Velocity{
				SpeedMps: v.speedMps,
				VX:       v.speedMps * math.Cos(v.headingDeg*math.Pi/180),
				VY:       v.speedMps * math.Sin(v.headingDeg*math.Pi/180),
			},
			HeadingDeg: v.headingDeg,
			MissionPayload: map[string]any{
				"payload_mode":      "OPTICAL_RECON",
				"target_tracking":   true,
				"ew_emitter_active": v.ewActive,
				"sensor_temp_c":     38.5,
			},
			Sequence: v.sequence,
		}

	case "HOVER":
		v.batteryPct = math.Max(5.0, v.batteryPct-(0.03*dt))
		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       "HOVER",
			BatteryPct:  math.Round(v.batteryPct*10) / 10,
			Coordinates: schema.Coordinates{
				Latitude:  v.centerLat,
				Longitude: v.centerLon,
				AltitudeM: v.currentAlt,
			},
			Velocity:   schema.Velocity{SpeedMps: 0.0},
			HeadingDeg: v.headingDeg,
			Sequence:   v.sequence,
		}

	case "RTB":
		v.currentAlt = math.Max(0.0, v.currentAlt-(10.0*dt))
		v.batteryPct = math.Max(5.0, v.batteryPct-(0.04*dt))
		if v.currentAlt <= 5.0 {
			v.state = "LANDED"
		}
		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       "RTB",
			BatteryPct:  math.Round(v.batteryPct*10) / 10,
			Coordinates: schema.Coordinates{
				Latitude:  v.centerLat,
				Longitude: v.centerLon,
				AltitudeM: v.currentAlt,
			},
			Velocity: schema.Velocity{
				SpeedMps: 8.0,
				VZ:       -2.0,
			},
			HeadingDeg: 0.0,
			Sequence:   v.sequence,
		}

	default:
		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       v.state,
			BatteryPct:  math.Round(v.batteryPct*10) / 10,
			Coordinates: schema.Coordinates{
				Latitude:  v.centerLat,
				Longitude: v.centerLon,
				AltitudeM: 0.0,
			},
			Sequence: v.sequence,
		}
	}
}

// HandleCommand executes an operator C2 flight instruction.
func (v *VehicleSim) HandleCommand(cmd schema.CommandPayload) {
	v.mu.Lock()
	defer v.mu.Unlock()

	log.Printf("[EDGE AGENT %s] Received C2 Command: %s (ID: %s)", v.id, cmd.CommandType, cmd.CommandID)

	switch cmd.CommandType {
	case "RETURN_TO_BASE":
		v.state = "RTB"
	case "HOVER":
		v.state = "HOVER"
	case "PATROL":
		v.state = "PATROL"
	case "ARM":
		if v.state == "LANDED" {
			v.state = "AIRBORNE"
			v.currentAlt = 100.0
		}
	case "DISARM":
		v.state = "LANDED"
		v.currentAlt = 0.0
	case "TOGGLE_EW":
		v.ewActive = !v.ewActive
	}
}

func main() {
	vehicleID := flag.String("id", "bravo", "Vehicle callsign/ID")
	vehicleType := flag.String("type", "drone", "Vehicle type")
	team := flag.String("team", "blue", "Team identifier")
	routerURL := flag.String("router", "http://127.0.0.1:8000", "Zenoh REST router URL")
	rateHz := flag.Float64("rate", 1.0, "Telemetry publication rate in Hz")
	emitTier3Periodic := flag.Bool("emit-tier3", false, "Periodically emit TIER-3 Sovereign packet to test CDS")
	mockBus := flag.Bool("mock", false, "Run in standalone mock mode")
	flag.Parse()

	log.Printf("Starting Edge Vehicle Companion Agent: [%s/%s/%s]", *vehicleType, *team, *vehicleID)

	var bus zenohutil.Bus
	if *mockBus {
		bus = zenohutil.NewMemoryBus()
	} else {
		bus = zenohutil.NewRESTClient(*routerURL)
	}
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sim := NewVehicleSim(*vehicleID, *vehicleType, *team, 37.7749, -122.4194)

	// Command listener subscription
	// Topic: sec/tier2/drone/blue/<id>/command
	cmdTopic := zenohutil.BuildKey("tier2", *vehicleType, *team, *vehicleID, "command")
	log.Printf("Listening for C2 commands on: %s", cmdTopic)
	err := bus.Subscribe(ctx, cmdTopic, func(key string, payload []byte) {
		env, err := zenohutil.ParseJSONEnvelope(payload)
		if err != nil || env.Command == nil {
			return
		}
		sim.HandleCommand(*env.Command)
	})
	if err != nil {
		log.Printf("[EDGE AGENT] Warning: Failed to subscribe to C2 commands: %v", err)
	}

	// Topic for tactical telemetry publication: sec/tier2/drone/blue/<id>/telemetry
	telemTopic := zenohutil.BuildKey("tier2", *vehicleType, *team, *vehicleID, "telemetry")

	interval := time.Duration(float64(time.Second) / *rateHz)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Printf("Publishing tactical telemetry to %s at %.1f Hz", telemTopic, *rateHz)

	count := 0
	for {
		select {
		case <-sigChan:
			log.Printf("Edge Agent %s stopping...", *vehicleID)
			return

		case <-ticker.C:
			count++
			telem := sim.Step(interval.Seconds())

			// Every 10th packet, if enabled, emit a TIER-3 Sovereign packet to test CDS Quarantine
			tier := schema.Tier2Restricted
			if *emitTier3Periodic && count%10 == 0 {
				tier = schema.Tier3Critical
				log.Printf("[EDGE AGENT %s] Emitting test TIER-3 CRITICAL Sovereign packet (CDS Fail-Closed Test)", *vehicleID)
			}

			pubKey := telemTopic
			if tier == schema.Tier3Critical {
				pubKey = zenohutil.BuildKey("tier3", *vehicleType, *team, *vehicleID, "telemetry")
			}

			env, err := schema.NewTelemetryEnvelope(tier, "edge-node-01-pi5", telem, nil)
			if err != nil {
				log.Printf("Error creating envelope: %v", err)
				continue
			}

			data, err := json.Marshal(env)
			if err != nil {
				log.Printf("Error serializing envelope: %v", err)
				continue
			}

			if err := bus.Publish(ctx, pubKey, data); err != nil {
				log.Printf("Publish error to %s: %v", pubKey, err)
			}
		}
	}
}
