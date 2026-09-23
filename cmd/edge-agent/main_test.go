package main

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

func TestVehicleSim_Initialization(t *testing.T) {
	v := NewVehicleSim("test-drone", "drone", "blue", 31.65, -8.01, 100.0, 0.01, 15.0)

	if v.id != "test-drone" || v.team != "blue" || v.state != "AIRBORNE" {
		t.Errorf("unexpected vehicle init: %+v", v)
	}
	if v.batteryPct != 98.5 {
		t.Errorf("expected initial battery 98.5, got %f", v.batteryPct)
	}
}

func TestVehicleSim_StepDynamics(t *testing.T) {
	v := NewVehicleSim("alpha", "drone", "blue", 31.65, -8.01, 120.0, 0.008, 14.5)

	initialBattery := v.batteryPct
	telem1 := v.Step(5.0)

	if telem1.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", telem1.Sequence)
	}
	if telem1.BatteryPct >= initialBattery {
		t.Errorf("expected battery to deplete after step, initial=%f current=%f", initialBattery, telem1.BatteryPct)
	}
	if telem1.Coordinates.Latitude == 0 || telem1.Coordinates.Longitude == 0 {
		t.Errorf("expected valid coordinates, got: %+v", telem1.Coordinates)
	}

	// Test HOVER state
	v.HandleCommand(schema.CommandPayload{CommandType: "HOVER"})
	telemHover := v.Step(1.0)
	if telemHover.State != "HOVER" {
		t.Errorf("expected HOVER state, got %s", telemHover.State)
	}
	if telemHover.Velocity.SpeedMps != 0.0 {
		t.Errorf("expected 0 speed during hover, got %f", telemHover.Velocity.SpeedMps)
	}

	// Test RTB state and landing descent
	v.HandleCommand(schema.CommandPayload{CommandType: "RETURN_TO_BASE"})
	telemRTB := v.Step(1.0)
	if telemRTB.State != "RTB" {
		t.Errorf("expected RTB state, got %s", telemRTB.State)
	}

	// Advance simulation through smooth RTB transit until landed
	for i := 0; i < 100 && telemRTB.State != "LANDED"; i++ {
		telemRTB = v.Step(2.0)
	}
	if telemRTB.State != "LANDED" {
		t.Errorf("expected vehicle to land after approaching base and descending, got: %s", telemRTB.State)
	}
}

func TestVehicleSim_ResumePatrolFromBase(t *testing.T) {
	v := NewVehicleSim("alpha", "drone", "blue", 31.625, -8.080, 100.0, 0.008, 14.5)
	v.SetBase(31.6120, -8.0850)

	// Command Return-To-Base and advance until landed
	v.HandleCommand(schema.CommandPayload{CommandType: "RETURN_TO_BASE"})
	telem := v.Step(1.0)
	for i := 0; i < 200 && telem.State != "LANDED"; i++ {
		telem = v.Step(2.0)
	}
	if telem.State != "LANDED" {
		t.Fatalf("expected drone to land at base pad, got state: %s", telem.State)
	}
	if telem.Coordinates.AltitudeM != 0.0 {
		t.Fatalf("expected altitude 0.0 on recovery pad, got %f", telem.Coordinates.AltitudeM)
	}

	// Command Resume Patrol
	v.HandleCommand(schema.CommandPayload{CommandType: "PATROL"})

	// Step 1: Initial liftoff from pad
	telemLiftoff := v.Step(1.0)
	if telemLiftoff.State != "TAKEOFF" {
		t.Errorf("expected TAKEOFF state during initial pad ascent, got: %s", telemLiftoff.State)
	}
	// Altitude must have climbed gradually, not snapped to 100m
	if telemLiftoff.Coordinates.AltitudeM <= 0.0 || telemLiftoff.Coordinates.AltitudeM > 25.0 {
		t.Errorf("expected gradual altitude climb on takeoff, got: %f", telemLiftoff.Coordinates.AltitudeM)
	}
	// Coordinates must be near base pad, NOT snapped 1.5km away to patrol orbit!
	distFromPadDeg := math.Hypot(telemLiftoff.Coordinates.Latitude-31.6120, telemLiftoff.Coordinates.Longitude-(-8.0850))
	if distFromPadDeg*111139.0 > 50.0 {
		t.Errorf("expected vehicle to remain near pad on initial liftoff, moved %f meters", distFromPadDeg*111139.0)
	}

	// Step through climb and forward transit
	telemTransit := v.Step(3.0)
	if telemTransit.State != "TRANSIT" {
		t.Errorf("expected TRANSIT state once airborne and cruising, got: %s", telemTransit.State)
	}

	// Advance until patrol orbit is reached
	for i := 0; i < 200 && telemTransit.State != "PATROL"; i++ {
		prevLat := telemTransit.Coordinates.Latitude
		prevLon := telemTransit.Coordinates.Longitude
		telemTransit = v.Step(1.0)
		// Ensure no instant coordinate snapping across ticks (< 30 meters per 1s tick)
		tickDistDeg := math.Hypot(telemTransit.Coordinates.Latitude-prevLat, telemTransit.Coordinates.Longitude-prevLon)
		if tickDistDeg*111139.0 > 30.0 {
			t.Fatalf("instant snapping detected during transit: jumped %f meters in 1 second", tickDistDeg*111139.0)
		}
	}

	if telemTransit.State != "PATROL" {
		t.Fatalf("expected vehicle to smoothly join PATROL orbit, got state: %s", telemTransit.State)
	}
	if telemTransit.Coordinates.AltitudeM < 95.0 {
		t.Errorf("expected cruise altitude around 100m in patrol, got %f", telemTransit.Coordinates.AltitudeM)
	}
}

func TestVehicleSim_CommandHandling(t *testing.T) {
	v := NewVehicleSim("bravo", "drone", "blue", 31.65, -8.01, 120.0, 0.008, 14.5)

	v.HandleCommand(schema.CommandPayload{CommandType: "HOVER"})
	if v.state != "HOVER" {
		t.Errorf("expected state HOVER, got %s", v.state)
	}

	v.HandleCommand(schema.CommandPayload{CommandType: "PATROL"})
	if v.state != "PATROL" {
		t.Errorf("expected state PATROL, got %s", v.state)
	}

	v.HandleCommand(schema.CommandPayload{CommandType: "DISARM"})
	if v.state != "LANDED" || v.currentAlt != 0.0 {
		t.Errorf("expected DISARM to land vehicle at 0 alt, got state=%s alt=%f", v.state, v.currentAlt)
	}

	v.HandleCommand(schema.CommandPayload{CommandType: "ARM"})
	if v.state != "AIRBORNE" || v.currentAlt != 100.0 {
		t.Errorf("expected ARM to transition to AIRBORNE, got state=%s alt=%f", v.state, v.currentAlt)
	}

	// Toggle EW
	initialEW := v.ewActive
	v.HandleCommand(schema.CommandPayload{CommandType: "TOGGLE_EW"})
	if v.ewActive == initialEW {
		t.Errorf("expected EW flag to toggle")
	}
}

func TestVehicleSim_PubSubIntegration(t *testing.T) {
	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sim := NewVehicleSim("charlie", "drone", "blue", 31.65, -8.01, 120.0, 0.008, 14.5)

	telemReceived := make(chan []byte, 10)
	telemTopic := zenohutil.BuildKey("tier2", "drone", "blue", "charlie", "telemetry")
	bus.Subscribe(ctx, telemTopic, func(key string, payload []byte) {
		telemReceived <- payload
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go runDroneInstance(ctx, sim, bus, 10.0, false, &wg)

	// Verify telemetry emission
	select {
	case data := <-telemReceived:
		env, err := zenohutil.ParseJSONEnvelope(data)
		if err != nil {
			t.Fatalf("parsing telemetry envelope: %v", err)
		}
		if env.Telemetry.VehicleID != "charlie" {
			t.Errorf("expected vehicle charlie, got %s", env.Telemetry.VehicleID)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for telemetry emission")
	}

	// Send C2 Command over Zenoh bus
	cmdPayload := schema.CommandPayload{
		CommandID:     "cmd-test-1",
		TargetVehicle: "charlie",
		CommandType:   "HOVER",
	}
	cmdEnv, err := schema.NewCommandEnvelope(schema.Tier2Restricted, "test-gcs", cmdPayload, nil)
	if err != nil {
		t.Fatalf("creating command envelope: %v", err)
	}
	cmdBytes, _ := json.Marshal(cmdEnv)
	cmdTopic := zenohutil.BuildKey("tier2", "drone", "blue", "charlie", "command")

	if err := bus.Publish(ctx, cmdTopic, cmdBytes); err != nil {
		t.Fatalf("publishing command: %v", err)
	}

	// Allow subscriber goroutine to process command
	time.Sleep(50 * time.Millisecond)

	sim.mu.RLock()
	st := sim.state
	sim.mu.RUnlock()
	if st != "HOVER" {
		t.Errorf("expected state to update to HOVER after command, got: %s", st)
	}

	cancel()
	wg.Wait()
}
