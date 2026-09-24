package main

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/mavlink"
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

func TestEdgeAgent_MAVLinkBridgeIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	vState := mavlink.NewVehicleState("alpha", "blue", "drone", schema.Tier2Restricted, 1)

	telemReceived := make(chan []byte, 10)
	telemTopic := zenohutil.BuildKey("tier2", "drone", "blue", "alpha", "telemetry")
	_ = bus.Subscribe(ctx, telemTopic, func(key string, payload []byte) {
		telemReceived <- payload
	})

	udpClient, err := mavlink.NewUDPClient("127.0.0.1:0", vState, func(env *schema.SecurityEnvelope) {
		data, err := json.Marshal(env)
		if err == nil {
			_ = bus.Publish(ctx, telemTopic, data)
		}
	})
	if err != nil {
		t.Fatalf("failed to bind udp listener: %v", err)
	}
	defer udpClient.Close()
	udpClient.Start(50 * time.Millisecond)

	// Send MAVLink packets from simulated SITL socket
	sitlConn, err := net.Dial("udp", udpClient.LocalAddr())
	if err != nil {
		t.Fatalf("failed to dial udp: %v", err)
	}
	defer sitlConn.Close()

	pos := &mavlink.GlobalPositionInt{
		Lat:         316500000,
		Lon:         -80100000,
		RelativeAlt: 20000, // 20m
		Vx:          1200,  // 12 m/s
		Hdg:         18000,
	}
	pkt, _ := mavlink.EncodeFrame(1, 1, 1, mavlink.MsgIDGlobalPositionInt, mavlink.EncodeGlobalPositionInt(pos))
	_, err = sitlConn.Write(pkt)
	if err != nil {
		t.Fatalf("failed to write to sitl socket: %v", err)
	}

	select {
	case data := <-telemReceived:
		env, err := zenohutil.ParseJSONEnvelope(data)
		if err != nil {
			t.Fatalf("failed to parse json envelope: %v", err)
		}
		if env.Telemetry.VehicleID != "alpha" {
			t.Errorf("expected vehicle alpha, got %s", env.Telemetry.VehicleID)
		}
		if env.Telemetry.Coordinates.AltitudeM != 20.0 {
			t.Errorf("expected alt 20.0m, got %f", env.Telemetry.Coordinates.AltitudeM)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mavlink telemetry via zenoh bus")
	}

	// Test C2 command delivery to SITL
	cmdTopic := zenohutil.BuildKey("tier2", "drone", "blue", "alpha", "command")
	_ = bus.Subscribe(ctx, cmdTopic, func(key string, payload []byte) {
		env, err := zenohutil.ParseJSONEnvelope(payload)
		if err != nil || env.Command == nil {
			return
		}
		if env.Command.CommandType == "RETURN_TO_BASE" {
			_ = udpClient.ReturnToLaunch()
		}
	})

	cmdEnv, _ := schema.NewCommandEnvelope(schema.Tier2Restricted, "test-gcs", schema.CommandPayload{
		CommandID:     "rtl-cmd-1",
		TargetVehicle: "alpha",
		CommandType:   "RETURN_TO_BASE",
	}, nil)
	cmdBytes, _ := json.Marshal(cmdEnv)
	_ = bus.Publish(ctx, cmdTopic, cmdBytes)

	// Read RTL command on SITL socket
	buf := make([]byte, 1024)
	_ = sitlConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := sitlConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read rtl command on sitl socket: %v", err)
	}

	frame, err := mavlink.DecodeFrame(buf[:n])
	if err != nil {
		t.Fatalf("failed to decode command frame: %v", err)
	}
	if frame.MessageID != mavlink.MsgIDCommandLong {
		t.Fatalf("expected MsgIDCommandLong, got %d", frame.MessageID)
	}
}

func TestRunMavlinkInstance_SerialFailSafe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	bus := zenohutil.NewMemoryBus()
	defer bus.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	// Attempting to open a non-existent serial port should log an error and exit gracefully without panic
	go runMavlinkInstance(ctx, "bravo", "blue", "drone", ":0", "/dev/nonexistent_test_port_12345", 115200, bus, &wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Exited gracefully as expected
	case <-time.After(1 * time.Second):
		t.Fatal("runMavlinkInstance did not exit gracefully on invalid serial port")
	}
}
