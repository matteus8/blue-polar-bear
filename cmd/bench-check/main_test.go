package main

import (
	"bytes"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/mavlink"
)

func TestBenchMetrics_Ingestion(t *testing.T) {
	metrics := &BenchMetrics{}

	// Create test stream with Heartbeat, Attitude, GlobalPositionInt, SysStatus
	hb := &mavlink.Heartbeat{
		Type:         2,
		Autopilot:    12,
		BaseMode:     0x80, // Armed
		SystemStatus: 4,
	}
	hbBytes, _ := mavlink.EncodeFrame(1, 1, 1, mavlink.MsgIDHeartbeat, mavlink.EncodeHeartbeat(hb))

	att := &mavlink.Attitude{
		TimeBootMs: 1000,
		Roll:       float32(5.0 * math.Pi / 180.0),
		Pitch:      float32(-2.5 * math.Pi / 180.0),
		Yaw:        float32(90.0 * math.Pi / 180.0),
	}
	attBytes, _ := mavlink.EncodeFrame(1, 1, 2, mavlink.MsgIDAttitude, mavlink.EncodeAttitude(att))

	pos := &mavlink.GlobalPositionInt{
		TimeBootMs:  1000,
		Lat:         316250000,
		Lon:         -80820000,
		RelativeAlt: 15000, // 15m
		Vx:          1000,  // 10 m/s
	}
	posBytes, _ := mavlink.EncodeFrame(1, 1, 3, mavlink.MsgIDGlobalPositionInt, mavlink.EncodeGlobalPositionInt(pos))

	sys := &mavlink.SysStatus{
		VoltageBattery:   16200, // 16.2V
		BatteryRemaining: 95,
	}
	sysBytes, _ := mavlink.EncodeFrame(1, 1, 4, mavlink.MsgIDSysStatus, mavlink.EncodeSysStatus(sys))

	var stream bytes.Buffer
	stream.Write(hbBytes)
	stream.Write(attBytes)
	stream.Write(posBytes)
	stream.Write(sysBytes)

	decoder := mavlink.NewStreamDecoder(&stream)

	// Process all frames
	for i := 0; i < 4; i++ {
		frame, err := decoder.NextFrame()
		if err != nil {
			t.Fatalf("failed decoding frame %d: %v", i, err)
		}
		atomic.AddUint64(&metrics.packetsTotal, 1)
		metrics.mu.Lock()
		metrics.lastPacket = time.Now()

		switch frame.MessageID {
		case mavlink.MsgIDHeartbeat:
			h, err := mavlink.DecodeHeartbeat(frame.Payload)
			if err == nil {
				metrics.armed = h.IsArmed()
			}
		case mavlink.MsgIDAttitude:
			a, err := mavlink.DecodeAttitude(frame.Payload)
			if err == nil {
				metrics.rollDeg = a.Roll * 180.0 / math.Pi
				metrics.pitchDeg = a.Pitch * 180.0 / math.Pi
				metrics.yawDeg = a.Yaw * 180.0 / math.Pi
			}
		case mavlink.MsgIDGlobalPositionInt:
			p, err := mavlink.DecodeGlobalPositionInt(frame.Payload)
			if err == nil {
				metrics.lat = p.LatDegrees()
				metrics.lon = p.LonDegrees()
				metrics.altM = p.AltMeters()
				metrics.speedMps = p.GroundSpeedMps()
			}
		case mavlink.MsgIDSysStatus:
			s, err := mavlink.DecodeSysStatus(frame.Payload)
			if err == nil {
				metrics.batteryV = float64(s.VoltageBattery) / 1000.0
				metrics.batteryPct = s.BatteryRemaining
			}
		}
		metrics.mu.Unlock()
	}

	if atomic.LoadUint64(&metrics.packetsTotal) != 4 {
		t.Errorf("expected 4 packets, got %d", metrics.packetsTotal)
	}
	if !metrics.armed {
		t.Errorf("expected armed=true")
	}
	if math.Abs(float64(metrics.rollDeg)-5.0) > 0.01 {
		t.Errorf("expected roll around 5.0 deg, got %f", metrics.rollDeg)
	}
	if math.Abs(metrics.lat-31.625) > 0.0001 {
		t.Errorf("expected lat 31.625, got %f", metrics.lat)
	}
	if metrics.batteryPct != 95 || math.Abs(metrics.batteryV-16.2) > 0.01 {
		t.Errorf("expected 16.2V 95%%, got %fV %d%%", metrics.batteryV, metrics.batteryPct)
	}
}
