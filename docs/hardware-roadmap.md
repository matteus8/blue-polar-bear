# Blue Polar Bear: Physical Hardware & Autonomy Roadmap

This document outlines the 4-phase progression bridging pure software simulation to physical autonomous flight on NDAA-compliant drone hardware.

---

## 1. Benchtop-to-Flight Progression Overview

```text
┌────────────────────────┐      ┌────────────────────────┐
│  Phase 1: Virtual SITL │ ───> │  Phase 2: Desk Pixhawk │
│  PX4 Sim over UDP:14550│      │  Hardware-in-the-Loop  │
└────────────────────────┘      └────────────────────────┘
            │                               │
            ▼                               ▼
┌────────────────────────┐      ┌────────────────────────┐
│ Phase 3: Companion SBC │ ───> │ Phase 4: Full Airframe │
│ Pi 5 UART + Zenoh Mesh │      │ Holybro X500 + Flight  │
└────────────────────────┘      └────────────────────────┘
```

---

## 2. Phased Integration Milestones

### Phase 1: Virtual Avionics (PX4 SITL on UDP `14550`)
- **Objective:** Connect `cmd/edge-agent` to the open-source PX4 Autopilot software simulator via MAVLink (`pkg/mavlink`).
- **Hardware Cost:** $0 (runs entirely in software on macOS / Linux).
- **Validation:** Verify that virtual GPS, battery levels, and attitude stream into `schema.SecurityEnvelope` without physical hardware.

### Phase 2: Benchtop Flight Controller (Desk Hardware-in-the-Loop)
- **Objective:** Connect `cmd/edge-agent` to a physical Pixhawk 6C / Cube flight controller over USB serial (`/dev/tty.usbmodem1` on macOS or `/dev/ttyACM0` on Linux) at 115200 / 921600 baud.
- **Hardware Cost:** ~$180–$210 (Desk Pixhawk 6C unit).
- **Driver Architecture:** Pure Go (`CGO_ENABLED=0`) POSIX termios driver (`pkg/mavlink/serial_darwin.go`, `pkg/mavlink/serial_linux.go`) with zero external runtime dependencies.
- **Stream Framing:** Continuous stream decoder (`pkg/mavlink/stream.go`) with line noise immunity, auto-synchronization on `0xFD` magic byte, and ITU X.25 CRC-16 integrity verification.
- **Bench Execution Command:**
  ```bash
  # macOS Benchtop Test
  ./bin/edge-agent -mavlink -mavlink-serial /dev/tty.usbmodem1 -mavlink-baud 115200 -id bench-1 -team blue

  # Linux (Pi 5 / Laptop) Benchtop Test
  ./bin/edge-agent -mavlink -mavlink-serial /dev/ttyACM0 -mavlink-baud 115200 -id bench-1 -team blue
  ```
- **Safety Precaution:** Propellers completely removed; unit operated on desk.
- **Validation:** Verify real physical IMU/accelerometer orientation and MAVLink C2 command response.

### Phase 3: Companion Single-Board Computer (Raspberry Pi 5 ARM64)
- **Objective:** Cross-compile `edge-agent` for Linux ARM64 (`GOOS=linux GOARCH=arm64`) and deploy as a resilient background system service.
- **Hardware Cost:** ~$90 (Pi 5 4GB/8GB + UART jumper harness).
- **Physical Interconnect Pinout (Pi 5 GPIO to Pixhawk 6C TELEM2):**
  | Raspberry Pi 5 Header | Pixhawk 6C TELEM2 Pin | Signal / Function |
  | :--- | :--- | :--- |
  | **Pin 8 (GPIO 14)** | **Pin 3 (RX)** | UART Transmit (Pi TX -> Pixhawk RX) |
  | **Pin 10 (GPIO 15)** | **Pin 2 (TX)** | UART Receive (Pixhawk TX -> Pi RX) |
  | **Pin 6 (GND)** | **Pin 6 (GND)** | Common Ground |
- **OS Configuration (Raspberry Pi OS 64-bit Bookworm):**
  ```bash
  # Enable UART hardware port /dev/ttyAMA0 and disable kernel serial console:
  sudo raspi-config nonint do_serial_cons 1
  sudo raspi-config nonint do_serial_hw 0
  ```
- **Automated Service Deployment:**
  ```bash
  # Install systemd unit and default configuration
  sudo cp deploy/systemd/edge-agent.service /etc/systemd/system/
  sudo cp deploy/systemd/edge-agent.default /etc/default/edge-agent
  sudo systemctl daemon-reload
  sudo systemctl enable --now edge-agent
  ```
- **Standalone Desk Sniffer (`cmd/bench-check`):**
  Inspect live IMU attitude, battery status, and GPS coordinates without running full GCS infrastructure:
  ```bash
  # Sniff serial packets directly from Pixhawk USB or UART
  ./bin/bench-check -serial /dev/ttyAMA0 -baud 921600

  # Sniff PX4 SITL UDP packets
  ./bin/bench-check -udp :14550
  ```
- **Validation:** Validate peer-to-peer Zenoh mesh connectivity over Wi-Fi / tactical RF to the GCS laptop.

### Phase 4: Full Airframe Integration & Flight Testing
- **Objective:** Mount the tested avionics deck onto an NDAA-compliant developer frame (Holybro X500 V2).
- **Hardware Cost:** ~$700 (airframe, motors, ESCs, LiPo battery, and DroneCAN Remote ID).
- **Regulatory Compliance:** Equip DroneCAN Remote ID broadcast module and register under FAA DroneZone.
- **Validation:** Execute controlled autonomous flight tests under FAA Part 107 / TRUST guidelines with Starlink Data Mule backhaul synchronization.
