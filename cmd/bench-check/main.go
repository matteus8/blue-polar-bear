package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/mavlink"
)

type BenchMetrics struct {
	mu           sync.RWMutex
	packetsTotal uint64
	crcErrors    uint64
	autopilot    string
	armed        bool
	systemStatus string
	rollDeg      float32
	pitchDeg     float32
	yawDeg       float32
	lat          float64
	lon          float64
	altM         float64
	speedMps     float64
	batteryV     float64
	batteryPct   int8
	lastPacket   time.Time
}

func main() {
	serialPort := flag.String("serial", "", "Serial port path (e.g. /dev/tty.usbmodem1 on macOS, /dev/ttyACM0 on Linux)")
	baud := flag.Int("baud", 115200, "Serial port baud rate (57600, 115200, 921600)")
	udpAddr := flag.String("udp", "", "UDP listening address for SITL simulation (e.g. :14550)")
	quiet := flag.Bool("quiet", false, "Disable periodic terminal refresh")
	flag.Parse()

	if *serialPort == "" && *udpAddr == "" {
		// Default to UDP 14550 if nothing specified
		*udpAddr = ":14550"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	metrics := &BenchMetrics{}

	var sourceLabel string
	var reader io.Reader
	var closer io.Closer

	if *serialPort != "" {
		sourceLabel = fmt.Sprintf("SERIAL %s @ %d baud", *serialPort, *baud)
		sp, err := mavlink.OpenSerialPort(*serialPort, *baud)
		if err != nil {
			log.Fatalf("[BENCH-CHECK] Failed to open serial port: %v", err)
		}
		reader = sp
		closer = sp
	} else {
		sourceLabel = fmt.Sprintf("UDP %s", *udpAddr)
		lAddr, err := net.ResolveUDPAddr("udp", *udpAddr)
		if err != nil {
			log.Fatalf("[BENCH-CHECK] Resolving UDP address %s: %v", *udpAddr, err)
		}
		uConn, err := net.ListenUDP("udp", lAddr)
		if err != nil {
			log.Fatalf("[BENCH-CHECK] Binding UDP socket %s: %v", *udpAddr, err)
		}
		reader = uConn
		closer = uConn
	}
	defer closer.Close()

	log.Printf("[BENCH-CHECK] Listening for MAVLink telemetry on %s...", sourceLabel)

	// Stream decoder
	decoder := mavlink.NewStreamDecoder(reader)

	// Ingestion goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			frame, err := decoder.NextFrame()
			if err != nil {
				if ctx.Err() != nil || err == io.EOF {
					return
				}
				atomic.AddUint64(&metrics.crcErrors, 1)
				time.Sleep(5 * time.Millisecond)
				continue
			}

			atomic.AddUint64(&metrics.packetsTotal, 1)
			metrics.mu.Lock()
			metrics.lastPacket = time.Now()

			switch frame.MessageID {
			case mavlink.MsgIDHeartbeat:
				hb, err := mavlink.DecodeHeartbeat(frame.Payload)
				if err == nil {
					metrics.armed = hb.IsArmed()
					metrics.autopilot = fmt.Sprintf("Type=%d Autopilot=%d", hb.Type, hb.Autopilot)
					metrics.systemStatus = fmt.Sprintf("Status=%d Mode=0x%02X", hb.SystemStatus, hb.BaseMode)
				}
			case mavlink.MsgIDAttitude:
				att, err := mavlink.DecodeAttitude(frame.Payload)
				if err == nil {
					metrics.rollDeg = att.Roll * 180.0 / math.Pi
					metrics.pitchDeg = att.Pitch * 180.0 / math.Pi
					metrics.yawDeg = att.Yaw * 180.0 / math.Pi
				}
			case mavlink.MsgIDGlobalPositionInt:
				pos, err := mavlink.DecodeGlobalPositionInt(frame.Payload)
				if err == nil {
					metrics.lat = pos.LatDegrees()
					metrics.lon = pos.LonDegrees()
					metrics.altM = pos.AltMeters()
					metrics.speedMps = pos.GroundSpeedMps()
				}
			case mavlink.MsgIDSysStatus:
				sys, err := mavlink.DecodeSysStatus(frame.Payload)
				if err == nil {
					metrics.batteryV = float64(sys.VoltageBattery) / 1000.0
					metrics.batteryPct = sys.BatteryRemaining
				}
			}
			metrics.mu.Unlock()
		}
	}()

	if *quiet {
		<-ctx.Done()
		return
	}

	// Live HUD refresh loop
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n[BENCH-CHECK] Terminating session. Summary:")
			fmt.Printf("  Total Packets: %d | CRC Drops: %d\n",
				atomic.LoadUint64(&metrics.packetsTotal),
				atomic.LoadUint64(&metrics.crcErrors))
			return
		case <-ticker.C:
			metrics.mu.RLock()
			total := atomic.LoadUint64(&metrics.packetsTotal)
			crcErr := atomic.LoadUint64(&metrics.crcErrors)
			armedStr := "DISARMED"
			if metrics.armed {
				armedStr = "ARMED"
			}
			age := "NONE"
			if !metrics.lastPacket.IsZero() {
				age = fmt.Sprintf("%0.1fs ago", time.Since(metrics.lastPacket).Seconds())
			}

			fmt.Print("\033[H\033[2J") // Clear terminal screen
			fmt.Println("================================================================================")
			fmt.Printf(" BLUE POLAR BEAR // DESK HITL AVIONICS SNIFFER & BENCHMARK\n")
			fmt.Printf(" Connection: %s | Packets Ingested: %d | CRC Drops: %d\n", sourceLabel, total, crcErr)
			fmt.Println("--------------------------------------------------------------------------------")
			fmt.Printf(" Autopilot:  %-24s | Armed: %-8s | Last Packet: %s\n", metrics.autopilot, armedStr, age)
			fmt.Printf(" Attitude:   Roll: %+06.2f°   Pitch: %+06.2f°   Yaw: %+06.2f°\n", metrics.rollDeg, metrics.pitchDeg, metrics.yawDeg)
			fmt.Printf(" Position:   Lat: %09.6f  Lon: %010.6f  Alt: %0.1fm  Speed: %0.1fm/s\n", metrics.lat, metrics.lon, metrics.altM, metrics.speedMps)
			fmt.Printf(" Battery:    %0.2fV (%d%%)\n", metrics.batteryV, metrics.batteryPct)
			fmt.Println("================================================================================")
			fmt.Println(" Press Ctrl+C to halt.")
			metrics.mu.RUnlock()
		}
	}
}
