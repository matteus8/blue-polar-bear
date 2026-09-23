package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

// VehicleSim encapsulates simulated flight dynamics and vehicle state.
type VehicleSim struct {
	mu          sync.RWMutex
	id          string
	vehicleType string
	team        string
	state       string
	batteryPct  float64
	centerLat   float64
	centerLon   float64
	baseLat     float64
	baseLon     float64
	currentLat  float64
	currentLon  float64
	currentAlt  float64
	speedMps    float64
	headingDeg  float64
	flightAngle float64
	orbitRadius float64
	sequence    uint64
	originNode  string
	ewActive    bool // Simulated electronic warfare / sovereign payload
}

func NewVehicleSim(id, vType, team string, lat, lon float64, alt, radius, speed float64) *VehicleSim {
	if alt <= 0 {
		alt = 120.0
	}
	if radius <= 0 {
		radius = 0.008
	}
	if speed <= 0 {
		speed = 14.5
	}
	startLat := roundFloat(lat+radius*math.Sin(0.0), 6)
	startLon := roundFloat(lon+radius*math.Cos(0.0), 6)
	return &VehicleSim{
		id:          id,
		vehicleType: vType,
		team:        team,
		state:       "AIRBORNE",
		batteryPct:  98.5,
		centerLat:   lat,
		centerLon:   lon,
		baseLat:     lat,
		baseLon:     lon,
		currentLat:  startLat,
		currentLon:  startLon,
		currentAlt:  alt,
		speedMps:    speed,
		headingDeg:  90.0,
		flightAngle: 0.0,
		orbitRadius: radius,
		originNode:  fmt.Sprintf("edge-node-%s-%s", team, id),
	}
}

func roundFloat(val float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(val*pow) / pow
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
		v.currentLat = roundFloat(v.centerLat+v.orbitRadius*math.Sin(v.flightAngle), 6)
		v.currentLon = roundFloat(v.centerLon+v.orbitRadius*math.Cos(v.flightAngle), 6)
		v.headingDeg = roundFloat(math.Mod((v.flightAngle*180/math.Pi)+90, 360), 1)
		v.batteryPct = roundFloat(math.Max(5.0, v.batteryPct-(0.04*dt)), 1)
		alt := roundFloat(v.currentAlt+5.0*math.Sin(v.flightAngle*2), 2)
		vx := roundFloat(v.speedMps*math.Cos(v.headingDeg*math.Pi/180), 2)
		vy := roundFloat(v.speedMps*math.Sin(v.headingDeg*math.Pi/180), 2)

		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       v.state,
			BatteryPct:  v.batteryPct,
			Coordinates: schema.Coordinates{
				Latitude:  v.currentLat,
				Longitude: v.currentLon,
				AltitudeM: alt,
			},
			Velocity: schema.Velocity{
				SpeedMps: roundFloat(v.speedMps, 2),
				VX:       vx,
				VY:       vy,
			},
			HeadingDeg: v.headingDeg,
			MissionPayload: map[string]any{
				"payload_mode":      "TACTICAL_RECON",
				"target_tracking":   true,
				"ew_emitter_active": v.ewActive,
				"sensor_temp_c":     38.5,
			},
			Sequence: v.sequence,
		}

	case "HOVER":
		// Loiter at current coordinates without snapping
		v.batteryPct = roundFloat(math.Max(5.0, v.batteryPct-(0.02*dt)), 1)
		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       "HOVER",
			BatteryPct:  v.batteryPct,
			Coordinates: schema.Coordinates{
				Latitude:  v.currentLat,
				Longitude: v.currentLon,
				AltitudeM: roundFloat(v.currentAlt, 1),
			},
			Velocity:   schema.Velocity{SpeedMps: 0.0},
			HeadingDeg: v.headingDeg,
			Sequence:   v.sequence,
		}

	case "RTB":
		// Realistic Return-To-Base transit flight:
		// Fly smoothly from current position towards base coordinates along vector
		dLat := v.baseLat - v.currentLat
		dLon := v.baseLon - v.currentLon
		distDeg := math.Sqrt(dLat*dLat + dLon*dLon)
		distM := distDeg * 111139.0 // meters

		v.batteryPct = roundFloat(math.Max(5.0, v.batteryPct-(0.03*dt)), 1)

		if distM > 10.0 {
			// In transit towards base
			bearingRad := math.Atan2(dLon, dLat)
			v.headingDeg = roundFloat(math.Mod((bearingRad*180/math.Pi)+360, 360), 1)

			// Step distance in degrees
			stepDeg := (v.speedMps * dt) / 111139.0
			if stepDeg >= distDeg {
				v.currentLat = v.baseLat
				v.currentLon = v.baseLon
			} else {
				v.currentLat = roundFloat(v.currentLat+stepDeg*math.Cos(bearingRad), 6)
				v.currentLon = roundFloat(v.currentLon+stepDeg*math.Sin(bearingRad), 6)
			}

			// Begin gradual descent when approaching base (< 200m)
			if distM < 200.0 && v.currentAlt > 15.0 {
				v.currentAlt = math.Max(15.0, v.currentAlt-(6.0*dt))
			}

			vx := roundFloat(v.speedMps*math.Sin(bearingRad), 2)
			vy := roundFloat(v.speedMps*math.Cos(bearingRad), 2)

			return schema.TelemetryPayload{
				VehicleID:   v.id,
				VehicleType: v.vehicleType,
				Team:        v.team,
				State:       "RTB",
				BatteryPct:  v.batteryPct,
				Coordinates: schema.Coordinates{
					Latitude:  v.currentLat,
					Longitude: v.currentLon,
					AltitudeM: roundFloat(v.currentAlt, 1),
				},
				Velocity: schema.Velocity{
					SpeedMps: roundFloat(v.speedMps, 1),
					VX:       vx,
					VY:       vy,
					VZ:       -1.0,
				},
				HeadingDeg: v.headingDeg,
				Sequence:   v.sequence,
			}
		}

		// Arrived at base: final touchdown descent
		v.speedMps = 0.0
		v.currentAlt = math.Max(0.0, v.currentAlt-(10.0*dt))
		if v.currentAlt <= 5.0 {
			v.state = "LANDED"
			v.currentAlt = 0.0
		}

		return schema.TelemetryPayload{
			VehicleID:   v.id,
			VehicleType: v.vehicleType,
			Team:        v.team,
			State:       v.state,
			BatteryPct:  v.batteryPct,
			Coordinates: schema.Coordinates{
				Latitude:  v.baseLat,
				Longitude: v.baseLon,
				AltitudeM: roundFloat(v.currentAlt, 1),
			},
			Velocity: schema.Velocity{
				SpeedMps: 0.0,
				VZ:       -2.0,
			},
			HeadingDeg: v.headingDeg,
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
				Latitude:  v.currentLat,
				Longitude: v.currentLon,
				AltitudeM: 0.0,
			},
			Sequence: v.sequence,
		}
	}
}

// SetBase assigns a dedicated recovery pad coordinate for the vehicle.
func (v *VehicleSim) SetBase(baseLat, baseLon float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.baseLat = baseLat
	v.baseLon = baseLon
}

// HandleCommand executes an operator C2 flight instruction.
func (v *VehicleSim) HandleCommand(cmd schema.CommandPayload) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Strict Target Vehicle Validation:
	// A drone must only execute commands explicitly addressed to its callsign/ID
	if cmd.TargetVehicle != "" && !strings.EqualFold(cmd.TargetVehicle, v.id) {
		trimmedTarget := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(cmd.TargetVehicle), "blue-"), "red-")
		trimmedID := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(v.id), "blue-"), "red-")
		if trimmedTarget != trimmedID {
			return
		}
	}

	log.Printf("[EDGE AGENT %s (%s)] Executing C2 Command: %s (ID: %s)", v.id, v.team, cmd.CommandType, cmd.CommandID)

	switch cmd.CommandType {
	case "RETURN_TO_BASE":
		v.state = "RTB"
		v.speedMps = 14.5
	case "HOVER":
		v.state = "HOVER"
		v.speedMps = 0.0
	case "PATROL":
		v.state = "PATROL"
		v.speedMps = 14.5
		if v.currentAlt < 30.0 {
			v.currentAlt = 100.0
		}
		// Synchronize flight angle with current position relative to center
		dLat := v.currentLat - v.centerLat
		dLon := v.currentLon - v.centerLon
		v.flightAngle = math.Atan2(dLat, dLon)
	case "ARM":
		if v.state == "LANDED" {
			v.state = "AIRBORNE"
			v.currentAlt = 100.0
			v.speedMps = 14.5
		}
	case "DISARM":
		v.state = "LANDED"
		v.currentAlt = 0.0
		v.speedMps = 0.0
	case "TOGGLE_EW":
		v.ewActive = !v.ewActive
	}
}

// runDroneInstance executes the publish and command listener loop for a single drone.
func runDroneInstance(ctx context.Context, sim *VehicleSim, bus zenohutil.Bus, rateHz float64, emitTier3Periodic bool, wg *sync.WaitGroup) {
	defer wg.Done()

	// Command topic: sec/tier2/drone/<team>/<id>/command
	cmdTopic := zenohutil.BuildKey("tier2", sim.vehicleType, sim.team, sim.id, "command")
	err := bus.Subscribe(ctx, cmdTopic, func(key string, payload []byte) {
		if !zenohutil.MatchesSelector(cmdTopic, key) {
			return
		}
		env, err := zenohutil.ParseJSONEnvelope(payload)
		if err != nil || env.Command == nil {
			return
		}
		sim.HandleCommand(*env.Command)
	})
	if err != nil {
		log.Printf("[EDGE %s] Failed to subscribe to C2 commands: %v", sim.id, err)
	}

	telemTopic := zenohutil.BuildKey("tier2", sim.vehicleType, sim.team, sim.id, "telemetry")
	interval := time.Duration(float64(time.Second) / rateHz)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count++
			telem := sim.Step(interval.Seconds())

			tier := schema.Tier2Restricted
			if emitTier3Periodic && count%15 == 0 {
				tier = schema.Tier3Critical
				log.Printf("[EDGE AGENT %s] Emitting test TIER-3 CRITICAL Sovereign packet (CDS Fail-Closed Test)", sim.id)
			}

			pubKey := telemTopic
			if tier == schema.Tier3Critical {
				pubKey = zenohutil.BuildKey("tier3", sim.vehicleType, sim.team, sim.id, "telemetry")
			}

			env, err := schema.NewTelemetryEnvelope(tier, sim.originNode, telem, nil)
			if err != nil {
				continue
			}

			data, err := json.Marshal(env)
			if err != nil {
				continue
			}

			_ = bus.Publish(ctx, pubKey, data)
		}
	}
}

func main() {
	swarmMode := flag.Bool("swarm", false, "Run multi-drone swarm simulation (5 Blue, 5 Red drones)")
	blueCount := flag.Int("blue-count", 5, "Number of Blue team drones in swarm")
	redCount := flag.Int("red-count", 5, "Number of Red team drones in swarm")
	singleID := flag.String("id", "bravo", "Single vehicle callsign/ID (used when swarm=false)")
	singleType := flag.String("type", "drone", "Single vehicle type")
	singleTeam := flag.String("team", "blue", "Single vehicle team")
	routerURL := flag.String("router", "http://127.0.0.1:8000", "Zenoh REST router URL")
	rateHz := flag.Float64("rate", 1.0, "Telemetry publication rate in Hz")
	emitTier3Periodic := flag.Bool("emit-tier3", false, "Periodically emit TIER-3 Sovereign packet to test CDS")
	mockBus := flag.Bool("mock", false, "Run in standalone mock mode")
	flag.Parse()

	var bus zenohutil.Bus
	if *mockBus {
		log.Printf("Edge Agent using In-Memory Bus")
		bus = zenohutil.NewMemoryBus()
	} else {
		log.Printf("Edge Agent connecting to Zenoh router: %s", *routerURL)
		bus = zenohutil.NewRESTClient(*routerURL)
	}
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	if *swarmMode {
		log.Printf("=================================================================")
		log.Printf(" LAUNCHING TACTICAL SWARM SIMULATOR: %d BLUE DRONES | %d RED DRONES", *blueCount, *redCount)
		log.Printf("=================================================================")

		blueCallsigns := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"}
		// Spawn Blue Fleet (Friendly Forces)
		for i := 0; i < *blueCount; i++ {
			callsign := fmt.Sprintf("blue-%d", i+1)
			if i < len(blueCallsigns) {
				callsign = fmt.Sprintf("blue-%s", blueCallsigns[i])
			}
			// Stagger coordinates in friendly sector (Morocco - West Sector)
			lat := 31.6200 + (float64(i) * 0.006)
			lon := -8.0800 - (float64(i) * 0.005)
			alt := 100.0 + (float64(i) * 15.0)
			radius := 0.006 + (float64(i) * 0.002)
			speed := 13.0 + (float64(i) * 1.2)

			// Dedicated recovery pad staggered safely in friendly base sector
			padLat := 31.6120 + (float64(i) * 0.002)
			padLon := -8.0850 - (float64(i) * 0.002)

			sim := NewVehicleSim(callsign, "drone", "blue", lat, lon, alt, radius, speed)
			sim.SetBase(padLat, padLon)
			wg.Add(1)
			go runDroneInstance(ctx, sim, bus, *rateHz, *emitTier3Periodic && (i == 0), &wg)
			log.Printf("  • Spawned Blue Drone: [%s] Orbit Center: (%.4f, %.4f) Pad: (%.4f, %.4f) Alt: %.0fm", callsign, lat, lon, padLat, padLon, alt)
		}

		// Spawn Red Fleet (Adversary Forces)
		for i := 0; i < *redCount; i++ {
			callsign := fmt.Sprintf("red-%d", i+1)
			// Stagger coordinates in adversary sector (Morocco - East Sector)
			lat := 31.6450 + (float64(i) * 0.006)
			lon := -7.9450 + (float64(i) * 0.005)
			alt := 110.0 + (float64(i) * 15.0)
			radius := 0.007 + (float64(i) * 0.002)
			speed := 14.0 + (float64(i) * 1.5)

			padLat := 31.6450 + (float64(i) * 0.002)
			padLon := -7.9300 + (float64(i) * 0.002)

			sim := NewVehicleSim(callsign, "drone", "red", lat, lon, alt, radius, speed)
			sim.SetBase(padLat, padLon)
			wg.Add(1)
			go runDroneInstance(ctx, sim, bus, *rateHz, false, &wg)
			log.Printf("  • Spawned Red Drone:  [%s] Orbit Center: (%.4f, %.4f) Pad: (%.4f, %.4f) Alt: %.0fm", callsign, lat, lon, padLat, padLon, alt)
		}
	} else {
		// Single Drone Mode (Morocco sector)
		log.Printf("Launching Single Vehicle: [%s/%s/%s]", *singleType, *singleTeam, *singleID)
		sim := NewVehicleSim(*singleID, *singleType, *singleTeam, 31.6300, -8.0500, 120.0, 0.008, 14.5)
		wg.Add(1)
		go runDroneInstance(ctx, sim, bus, *rateHz, *emitTier3Periodic, &wg)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutdown signal received. Stopping all drones...")
	cancel()
	wg.Wait()
	log.Printf("All edge agent simulations halted.")
}
