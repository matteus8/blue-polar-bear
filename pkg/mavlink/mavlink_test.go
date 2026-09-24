package mavlink

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

func TestCRC_Calculation(t *testing.T) {
	crc := NewX25CRC()
	crc.AccumulateBytes([]byte("123456789"))
	expected := uint16(0x6F91)
	if crc.Value() != expected {
		t.Fatalf("expected CRC 0x%04X, got 0x%04X", expected, crc.Value())
	}
}

func TestHeartbeat_EncodeDecode(t *testing.T) {
	hb := &Heartbeat{
		CustomMode:     12345,
		Type:           2,  // Quadrotor
		Autopilot:      12, // PX4
		BaseMode:       0x80 | 0x01, // Armed
		SystemStatus:   4,  // Active
		MavlinkVersion: 3,
	}

	payload := EncodeHeartbeat(hb)
	if len(payload) != 9 {
		t.Fatalf("expected payload len 9, got %d", len(payload))
	}

	decoded, err := DecodeHeartbeat(payload)
	if err != nil {
		t.Fatalf("decode heartbeat failed: %v", err)
	}

	if decoded.CustomMode != hb.CustomMode || decoded.Type != hb.Type || !decoded.IsArmed() {
		t.Fatalf("heartbeat mismatch: got %+v, want %+v", decoded, hb)
	}
}

func TestGlobalPositionInt_EncodeDecode(t *testing.T) {
	pos := &GlobalPositionInt{
		TimeBootMs:  50000,
		Lat:         316500000,  // 31.65 deg N
		Lon:         -80100000,  // -8.01 deg W
		Alt:         150000,     // 150m MSL
		RelativeAlt: 25000,      // 25m AGL
		Vx:          1200,       // 12 m/s North
		Vy:          500,        // 5 m/s East
		Vz:          0,
		Hdg:         18050,      // 180.5 deg
	}

	payload := EncodeGlobalPositionInt(pos)
	decoded, err := DecodeGlobalPositionInt(payload)
	if err != nil {
		t.Fatalf("decode global position failed: %v", err)
	}

	if decoded.Lat != pos.Lat || decoded.Lon != pos.Lon || decoded.RelativeAlt != pos.RelativeAlt {
		t.Fatalf("position mismatch: got %+v, want %+v", decoded, pos)
	}

	if decoded.LatDegrees() != 31.65 || decoded.LonDegrees() != -8.01 {
		t.Fatalf("coordinate degrees mismatch: lat=%f, lon=%f", decoded.LatDegrees(), decoded.LonDegrees())
	}

	if decoded.AltMeters() != 25.0 {
		t.Fatalf("altitude meters mismatch: alt=%f", decoded.AltMeters())
	}

	if decoded.HeadingDeg() != 180.5 {
		t.Fatalf("heading mismatch: hdg=%f", decoded.HeadingDeg())
	}
}

func TestFrame_EncodeDecodeWithCRC(t *testing.T) {
	pos := &GlobalPositionInt{
		TimeBootMs:  1000,
		Lat:         316500000,
		Lon:         -80100000,
		Alt:         10000,
		RelativeAlt: 10000,
		Vx:          0,
		Vy:          0,
		Vz:          0,
		Hdg:         9000,
	}
	payload := EncodeGlobalPositionInt(pos)

	frameBytes, err := EncodeFrame(1, 1, 42, MsgIDGlobalPositionInt, payload)
	if err != nil {
		t.Fatalf("encode frame failed: %v", err)
	}

	frame, err := DecodeFrame(frameBytes)
	if err != nil {
		t.Fatalf("decode frame failed: %v", err)
	}

	if frame.SystemID != 1 || frame.Sequence != 42 || frame.MessageID != MsgIDGlobalPositionInt {
		t.Fatalf("frame header mismatch: %+v", frame)
	}
}

func TestVehicleState_AggregatorAndEnvelope(t *testing.T) {
	vState := NewVehicleState("blue-alpha", "blue", "drone", schema.Tier2Restricted, 1)

	// Feed Heartbeat (Armed)
	hb := &Heartbeat{BaseMode: 0x80}
	hbBytes, _ := EncodeFrame(1, 1, 1, MsgIDHeartbeat, EncodeHeartbeat(hb))
	hbFrame, _ := DecodeFrame(hbBytes)
	_ = vState.IngestFrame(hbFrame)

	// Feed Position
	pos := &GlobalPositionInt{
		Lat:         316500000,
		Lon:         -80100000,
		RelativeAlt: 15000, // 15m
		Vx:          1000,
		Vy:          0,
		Hdg:         9000,
	}
	posBytes, _ := EncodeFrame(1, 1, 2, MsgIDGlobalPositionInt, EncodeGlobalPositionInt(pos))
	posFrame, _ := DecodeFrame(posBytes)
	_ = vState.IngestFrame(posFrame)

	// Feed Battery
	sys := &SysStatus{BatteryRemaining: 88}
	sysBytes, _ := EncodeFrame(1, 1, 3, MsgIDSysStatus, EncodeSysStatus(sys))
	sysFrame, _ := DecodeFrame(sysBytes)
	_ = vState.IngestFrame(sysFrame)

	lat, lon, alt, speed, hdg, batt, armed, status := vState.GetStateSnapshot()
	if !armed || status != "IN_FLIGHT" {
		t.Fatalf("expected armed IN_FLIGHT, got armed=%v, status=%s", armed, status)
	}
	if lat != 31.65 || lon != -8.01 || alt != 15.0 || batt != 88.0 || hdg != 90.0 || speed != 10.0 {
		t.Fatalf("snapshot mismatch: lat=%f, lon=%f, alt=%f, batt=%f, hdg=%f, speed=%f", lat, lon, alt, batt, hdg, speed)
	}

	env, err := vState.BuildSecurityEnvelope()
	if err != nil {
		t.Fatalf("build envelope failed: %v", err)
	}

	if err := env.Validate(); err != nil {
		t.Fatalf("envelope validation failed: %v", err)
	}

	if env.Header.Classification != schema.Tier2Restricted || env.Telemetry.VehicleID != "blue-alpha" {
		t.Fatalf("envelope header mismatch: %+v", env.Header)
	}
}

func TestUDPClient_Loopback(t *testing.T) {
	vState := NewVehicleState("blue-alpha", "blue", "drone", schema.Tier2Restricted, 1)

	envelopeCh := make(chan *schema.SecurityEnvelope, 10)
	client, err := NewUDPClient("127.0.0.1:0", vState, func(env *schema.SecurityEnvelope) {
		envelopeCh <- env
	})
	if err != nil {
		t.Fatalf("new udp client failed: %v", err)
	}
	defer client.Close()

	localAddr := client.conn.LocalAddr().String()
	client.Start(100 * time.Millisecond)

	// Send a mock MAVLink packet from a simulator socket
	simConn, err := net.Dial("udp", localAddr)
	if err != nil {
		t.Fatalf("dial udp failed: %v", err)
	}
	defer simConn.Close()

	pos := &GlobalPositionInt{
		Lat:         316500000,
		Lon:         -80100000,
		RelativeAlt: 18000,
		Hdg:         27000,
	}
	packet, _ := EncodeFrame(1, 1, 1, MsgIDGlobalPositionInt, EncodeGlobalPositionInt(pos))
	_, err = simConn.Write(packet)
	if err != nil {
		t.Fatalf("sim write failed: %v", err)
	}

	// Wait for published envelope
	select {
	case env := <-envelopeCh:
		if env.Telemetry.Coordinates.Latitude != 31.65 || env.Telemetry.Coordinates.AltitudeM != 18.0 {
			t.Fatalf("unexpected envelope payload: %+v", env.Telemetry)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for telemetry envelope from udp client")
	}

	// Test sending C2 command back to simulator
	err = client.ReturnToLaunch()
	if err != nil {
		t.Fatalf("return to launch command failed: %v", err)
	}

	// Simulator receives command packet
	buf := make([]byte, 1024)
	_ = simConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := simConn.Read(buf)
	if err != nil {
		t.Fatalf("simulator failed to read command: %v", err)
	}

	cmdFrame, err := DecodeFrame(buf[:n])
	if err != nil {
		t.Fatalf("failed to decode simulator command frame: %v", err)
	}

	if cmdFrame.MessageID != MsgIDCommandLong {
		t.Fatalf("expected CommandLong (76), got %d", cmdFrame.MessageID)
	}

	cmdNum := binary.LittleEndian.Uint16(cmdFrame.Payload[28:30])
	if cmdNum != MavCmdNavReturnToLaunch {
		t.Fatalf("expected MavCmdNavReturnToLaunch (20), got %d", cmdNum)
	}
}
