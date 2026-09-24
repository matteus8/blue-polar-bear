package mavlink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

// AutopilotClient defines a unified interface for flight controller communication
// across both UDP (PX4 SITL) and Serial / UART / Stream (Pixhawk 6C / Desk HITL) transports.
type AutopilotClient interface {
	Start(publishInterval time.Duration)
	SendCommandLong(cmd uint16, p1, p2, p3, p4, p5, p6, p7 float32) error
	ReturnToLaunch() error
	SetArmed(arm bool) error
	Close() error
}

var _ AutopilotClient = (*UDPClient)(nil)
var _ AutopilotClient = (*StreamClient)(nil)

// StreamClient manages a bidirectional stream connection (e.g. SerialPort, Pipe, TCP)
// to a physical flight controller or companion computer.
type StreamClient struct {
	mu           sync.RWMutex
	rwc          io.ReadWriteCloser
	decoder      *StreamDecoder
	vehicleState *VehicleState
	callback     TelemetryCallback
	ctx          context.Context
	cancel       context.CancelFunc
	seq          byte
}

// NewStreamClient initializes a MAVLink stream client wrapping any io.ReadWriteCloser.
func NewStreamClient(rwc io.ReadWriteCloser, vState *VehicleState, cb TelemetryCallback) *StreamClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamClient{
		rwc:          rwc,
		decoder:      NewStreamDecoder(rwc),
		vehicleState: vState,
		callback:     cb,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start launches the read loop and periodic telemetry publisher.
func (s *StreamClient) Start(publishInterval time.Duration) {
	go s.readLoop()
	go s.publishLoop(publishInterval)
}

// Close terminates background goroutines and closes the underlying stream/serial port.
func (s *StreamClient) Close() error {
	s.cancel()
	if s.rwc != nil {
		return s.rwc.Close()
	}
	return nil
}

// readLoop continuously reads and decodes MAVLink v2 frames from the stream.
func (s *StreamClient) readLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		frame, err := s.decoder.NextFrame()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				if errors.Is(err, io.EOF) {
					return
				}
				log.Printf("[MAVLINK STREAM] Read error: %v", err)
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		if err := s.vehicleState.IngestFrame(frame); err != nil {
			log.Printf("[MAVLINK STREAM] Error ingesting frame msgID=%d: %v", frame.MessageID, err)
		}
	}
}

// publishLoop periodically generates a SecurityEnvelope and invokes the callback.
func (s *StreamClient) publishLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.callback != nil {
				env, err := s.vehicleState.BuildSecurityEnvelope()
				if err == nil {
					s.callback(env)
				}
			}
		}
	}
}

// SendCommandLong transmits a MAV_CMD via COMMAND_LONG over the stream.
func (s *StreamClient) SendCommandLong(cmd uint16, p1, p2, p3, p4, p5, p6, p7 float32) error {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	cmdMsg := &CommandLong{
		Param1:          p1,
		Param2:          p2,
		Param3:          p3,
		Param4:          p4,
		Param5:          p5,
		Param6:          p6,
		Param7:          p7,
		Command:         cmd,
		TargetSystem:    1,
		TargetComponent: 1,
		Confirmation:    0,
	}

	payload := EncodeCommandLong(cmdMsg)
	packet, err := EncodeFrame(255, 190, seq, MsgIDCommandLong, payload)
	if err != nil {
		return fmt.Errorf("encoding command frame: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rwc == nil {
		return errors.New("stream closed")
	}
	_, err = s.rwc.Write(packet)
	if err != nil {
		return fmt.Errorf("writing command to stream: %w", err)
	}
	return nil
}

// ReturnToLaunch dispatches MAV_CMD_NAV_RETURN_TO_LAUNCH (20) to the autopilot.
func (s *StreamClient) ReturnToLaunch() error {
	return s.SendCommandLong(MavCmdNavReturnToLaunch, 0, 0, 0, 0, 0, 0, 0)
}

// SetArmed sends MAV_CMD_COMPONENT_ARM_DISARM (400) with param1=1 (arm) or param1=0 (disarm).
func (s *StreamClient) SetArmed(arm bool) error {
	var val float32
	if arm {
		val = 1.0
	}
	return s.SendCommandLong(MavCmdComponentArmDisarm, val, 0, 0, 0, 0, 0, 0)
}
