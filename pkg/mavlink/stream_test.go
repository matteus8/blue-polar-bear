package mavlink

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

func TestStreamDecoder_CleanStream(t *testing.T) {
	// Encode two valid packets
	hb := &Heartbeat{
		CustomMode:     4,
		Type:           2,
		Autopilot:      12,
		BaseMode:       0x80,
		SystemStatus:   4,
		MavlinkVersion: 3,
	}
	hbBytes, err := EncodeFrame(1, 1, 1, MsgIDHeartbeat, EncodeHeartbeat(hb))
	if err != nil {
		t.Fatalf("failed encoding heartbeat: %v", err)
	}

	pos := &GlobalPositionInt{
		TimeBootMs:  123456,
		Lat:         316200000,
		Lon:         -80800000,
		RelativeAlt: 120500,
		Hdg:         9000,
	}
	posBytes, err := EncodeFrame(1, 1, 2, MsgIDGlobalPositionInt, EncodeGlobalPositionInt(pos))
	if err != nil {
		t.Fatalf("failed encoding position: %v", err)
	}

	var stream bytes.Buffer
	stream.Write(hbBytes)
	stream.Write(posBytes)

	decoder := NewStreamDecoder(&stream)

	// Decode Frame 1
	f1, err := decoder.NextFrame()
	if err != nil {
		t.Fatalf("expected first frame, got error: %v", err)
	}
	if f1.MessageID != MsgIDHeartbeat {
		t.Fatalf("expected MsgIDHeartbeat (0), got %d", f1.MessageID)
	}

	// Decode Frame 2
	f2, err := decoder.NextFrame()
	if err != nil {
		t.Fatalf("expected second frame, got error: %v", err)
	}
	if f2.MessageID != MsgIDGlobalPositionInt {
		t.Fatalf("expected MsgIDGlobalPositionInt (33), got %d", f2.MessageID)
	}

	// Next should return io.EOF
	_, err = decoder.NextFrame()
	if err != io.EOF {
		t.Fatalf("expected io.EOF at end of stream, got %v", err)
	}
}

func TestStreamDecoder_WithLineNoiseAndCorruptedBytes(t *testing.T) {
	// Create valid packet
	pos := &GlobalPositionInt{
		TimeBootMs:  50000,
		Lat:         316200000,
		Lon:         -80800000,
		RelativeAlt: 100000,
		Hdg:         18000,
	}
	validBytes, err := EncodeFrame(1, 1, 1, MsgIDGlobalPositionInt, EncodeGlobalPositionInt(pos))
	if err != nil {
		t.Fatalf("failed encoding position: %v", err)
	}

	var stream bytes.Buffer
	// Noise prefix (random UART garbage, 0x00, 0xFF, false 0xFD with bad CRC)
	stream.Write([]byte{0x55, 0xAA, 0x00, 0xFF})
	// False 0xFD byte followed by junk
	stream.Write([]byte{0xFD, 0x05, 0x00, 0x00, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x00, 0x00})
	// Valid packet
	stream.Write(validBytes)
	// Noise suffix
	stream.Write([]byte{0xFE, 0x00, 0x12})

	decoder := NewStreamDecoder(&stream)
	f, err := decoder.NextFrame()
	if err != nil {
		t.Fatalf("failed to recover valid frame from noisy stream: %v", err)
	}
	if f.MessageID != MsgIDGlobalPositionInt {
		t.Fatalf("expected MsgIDGlobalPositionInt, got %d", f.MessageID)
	}
}

func TestStreamDecoder_FragmentedChunks(t *testing.T) {
	hb := &Heartbeat{
		CustomMode:     3,
		Type:           2,
		Autopilot:      12,
		BaseMode:       0x80,
		SystemStatus:   4,
		MavlinkVersion: 3,
	}
	packetBytes, err := EncodeFrame(1, 1, 42, MsgIDHeartbeat, EncodeHeartbeat(hb))
	if err != nil {
		t.Fatalf("failed encoding heartbeat: %v", err)
	}

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		// Write byte-by-byte with small delays to simulate slow serial UART line
		for _, b := range packetBytes {
			_, _ = pw.Write([]byte{b})
			time.Sleep(1 * time.Millisecond)
		}
	}()

	decoder := NewStreamDecoder(pr)
	frame, err := decoder.NextFrame()
	if err != nil {
		t.Fatalf("expected valid frame from fragmented stream, got error: %v", err)
	}
	if frame.MessageID != MsgIDHeartbeat {
		t.Fatalf("expected MsgIDHeartbeat, got %d", frame.MessageID)
	}
	if frame.Sequence != 42 {
		t.Fatalf("expected sequence 42, got %d", frame.Sequence)
	}
}

// pipeReadWriteCloser combines a PipeReader and a PipeWriter to mock a full-duplex serial port.
type pipeReadWriteCloser struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *pipeReadWriteCloser) Read(b []byte) (n int, err error) {
	return p.reader.Read(b)
}

func (p *pipeReadWriteCloser) Write(b []byte) (n int, err error) {
	return p.writer.Write(b)
}

func (p *pipeReadWriteCloser) Close() error {
	_ = p.reader.Close()
	return p.writer.Close()
}

func TestStreamClient_Bidirectional(t *testing.T) {
	// drone writes to autopilotRx, reads from autopilotTx
	autopilotRxReader, autopilotRxWriter := io.Pipe()
	autopilotTxReader, autopilotTxWriter := io.Pipe()

	clientRWC := &pipeReadWriteCloser{
		reader: autopilotTxReader,
		writer: autopilotRxWriter,
	}

	vState := NewVehicleState("test-drone", "blue", "drone", schema.Tier2Restricted, 1)

	var mu sync.Mutex
	var receivedEnvelopes []*schema.SecurityEnvelope

	client := NewStreamClient(clientRWC, vState, func(env *schema.SecurityEnvelope) {
		mu.Lock()
		defer mu.Unlock()
		receivedEnvelopes = append(receivedEnvelopes, env)
	})
	client.Start(50 * time.Millisecond)
	defer client.Close()

	// Autopilot sends HEARTBEAT and GLOBAL_POSITION_INT over autopilotTx
	hb := &Heartbeat{
		CustomMode:   3,
		Type:         2,
		BaseMode:     0x80, // Armed
		SystemStatus: 4,
	}
	hbPacket, _ := EncodeFrame(1, 1, 1, MsgIDHeartbeat, EncodeHeartbeat(hb))
	_, _ = autopilotTxWriter.Write(hbPacket)

	pos := &GlobalPositionInt{
		TimeBootMs:  1000,
		Lat:         316200000,
		Lon:         -80800000,
		RelativeAlt: 110000,
		Hdg:         9000,
		Vx:          1000, // 10 m/s
	}
	posPacket, _ := EncodeFrame(1, 1, 2, MsgIDGlobalPositionInt, EncodeGlobalPositionInt(pos))
	_, _ = autopilotTxWriter.Write(posPacket)

	// Wait for telemetry publisher to fire
	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	count := len(receivedEnvelopes)
	var latestEnv *schema.SecurityEnvelope
	if count > 0 {
		latestEnv = receivedEnvelopes[count-1]
	}
	mu.Unlock()

	if count == 0 || latestEnv == nil {
		t.Fatalf("expected at least 1 telemetry envelope from StreamClient")
	}

	if latestEnv.Telemetry.VehicleID != "test-drone" {
		t.Errorf("expected vehicleID test-drone, got %s", latestEnv.Telemetry.VehicleID)
	}
	if latestEnv.Telemetry.Coordinates.Latitude != 31.62 {
		t.Errorf("expected lat 31.62, got %f", latestEnv.Telemetry.Coordinates.Latitude)
	}

	// Test Command Egress: Send ReturnToLaunch
	cmdErrChan := make(chan error, 1)
	go func() {
		cmdErrChan <- client.ReturnToLaunch()
	}()

	// Autopilot reads command from autopilotRxReader
	decoder := NewStreamDecoder(autopilotRxReader)
	cmdFrame, err := decoder.NextFrame()
	if err != nil {
		t.Fatalf("failed to read dispatched command frame from stream: %v", err)
	}
	if cmdFrame.MessageID != MsgIDCommandLong {
		t.Fatalf("expected MsgIDCommandLong (76), got %d", cmdFrame.MessageID)
	}

	if err := <-cmdErrChan; err != nil {
		t.Fatalf("client.ReturnToLaunch failed: %v", err)
	}
}

func TestOpenSerialPort_EmptyPath(t *testing.T) {
	_, err := OpenSerialPort("", 115200)
	if err == nil {
		t.Fatal("expected error when opening serial port with empty path, got nil")
	}
}
