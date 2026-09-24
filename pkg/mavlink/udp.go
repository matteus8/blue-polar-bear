package mavlink

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

// TelemetryCallback is invoked whenever a new SecurityEnvelope is generated from MAVLink data.
type TelemetryCallback func(envelope *schema.SecurityEnvelope)

// UDPClient manages a bidirectional UDP socket connected to PX4 SITL or physical flight controller.
type UDPClient struct {
	mu           sync.RWMutex
	conn         *net.UDPConn
	remoteAddr   *net.UDPAddr
	vehicleState *VehicleState
	callback     TelemetryCallback
	ctx          context.Context
	cancel       context.CancelFunc
	seq          byte
}

// NewUDPClient creates and binds a UDP listener on the specified local address (e.g., "0.0.0.0:14550").
func NewUDPClient(listenAddr string, vState *VehicleState, cb TelemetryCallback) (*UDPClient, error) {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("resolving udp listen addr %s: %w", listenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on udp %s: %w", listenAddr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &UDPClient{
		conn:         conn,
		vehicleState: vState,
		callback:     cb,
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

// Start begins reading UDP packets asynchronously and starts periodic telemetry publishing.
func (u *UDPClient) Start(publishInterval time.Duration) {
	go u.readLoop()
	go u.publishLoop(publishInterval)
}

// LocalAddr returns the actual listening address of the UDP socket.
func (u *UDPClient) LocalAddr() string {
	if u.conn != nil {
		return u.conn.LocalAddr().String()
	}
	return ""
}

// Close terminates the listener and closes the UDP socket.
func (u *UDPClient) Close() error {
	u.cancel()
	if u.conn != nil {
		return u.conn.Close()
	}
	return nil
}

// readLoop continuously reads incoming datagrams from PX4 SITL / flight controller.
func (u *UDPClient) readLoop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-u.ctx.Done():
			return
		default:
		}

		_ = u.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remoteAddr, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-u.ctx.Done():
				return
			default:
				log.Printf("[MAVLINK UDP] Read error: %v", err)
				continue
			}
		}

		u.mu.Lock()
		u.remoteAddr = remoteAddr
		u.mu.Unlock()

		frame, err := DecodeFrame(buf[:n])
		if err != nil {
			continue // Skip unparseable non-MAVLink packets
		}

		if err := u.vehicleState.IngestFrame(frame); err != nil {
			log.Printf("[MAVLINK UDP] Error ingesting frame msgID=%d: %v", frame.MessageID, err)
		}
	}
}

// publishLoop periodically generates a SecurityEnvelope and triggers the telemetry callback.
func (u *UDPClient) publishLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-u.ctx.Done():
			return
		case <-ticker.C:
			if u.callback != nil {
				env, err := u.vehicleState.BuildSecurityEnvelope()
				if err == nil {
					u.callback(env)
				}
			}
		}
	}
}

// SendCommandLong transmits a MAV_CMD via COMMAND_LONG to the autopilot.
func (u *UDPClient) SendCommandLong(cmd uint16, p1, p2, p3, p4, p5, p6, p7 float32) error {
	u.mu.Lock()
	rAddr := u.remoteAddr
	u.seq++
	seq := u.seq
	u.mu.Unlock()

	if rAddr == nil {
		return fmt.Errorf("cannot send command: no remote autopilot packet received yet to determine target address")
	}

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

	_, err = u.conn.WriteToUDP(packet, rAddr)
	if err != nil {
		return fmt.Errorf("writing udp command: %w", err)
	}

	return nil
}

// ReturnToLaunch dispatches MAV_CMD_NAV_RETURN_TO_LAUNCH (20) to the autopilot.
func (u *UDPClient) ReturnToLaunch() error {
	return u.SendCommandLong(MavCmdNavReturnToLaunch, 0, 0, 0, 0, 0, 0, 0)
}

// SetArmed sends MAV_CMD_COMPONENT_ARM_DISARM (400) with param1=1 (arm) or param1=0 (disarm).
func (u *UDPClient) SetArmed(arm bool) error {
	var val float32
	if arm {
		val = 1.0
	}
	return u.SendCommandLong(MavCmdComponentArmDisarm, val, 0, 0, 0, 0, 0, 0)
}
