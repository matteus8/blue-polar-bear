package main

import (
	"context"
	"encoding/json"
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

	// Advance simulation until landed
	for i := 0; i < 20; i++ {
		telemRTB = v.Step(1.0)
	}
	if telemRTB.State != "LANDED" {
		t.Errorf("expected vehicle to land after altitude reaches <= 5m, got: %s", telemRTB.State)
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
