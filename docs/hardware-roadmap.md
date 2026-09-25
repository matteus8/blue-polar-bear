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

---

## 3. Core Avionics Terminology: HITL, Sniffer & HUD

To bridge software and embedded hardware engineering, `cmd/bench-check` implements three foundational concepts:

### 1. HITL (Hardware-in-the-Loop)
* **SITL (Software-in-the-Loop):** The flight physics, sensor models, and autopilot code all execute as software inside CPU processes (e.g. PX4 SITL over UDP).
* **HITL (Hardware-in-the-Loop):** The autopilot code runs on **real, physical embedded flight hardware** (such as an STM32H7 processor on a Pixhawk 6C). 
* **"On Desk" HITL:** The flight controller sits on your office desk connected via USB-C or UART jumper wires with propellers and motors disconnected. Tilting the board with your hand exercises the physical onboard MEMS gyroscopes, accelerometers, and magnetometers, streaming live physical telemetry down to the host computer.

### 2. Sniffer (Protocol Packet Analyzer)
* A **Sniffer** passively monitors a raw communication channel (such as a serial COM port or network socket) without injecting commands or altering the traffic.
* `cmd/bench-check` acts as an automated MAVLink v2 protocol analyzer. It captures continuous byte streams, hunts for the `0xFD` framing byte, extracts message IDs, computes ITU X.25 CRC-16 checksums, and tallies packet reception rates and corruption drops.

### 3. HUD (Heads-Up Display)
* Borrowed from tactical aviation, a **HUD** projects critical spatial orientation and vehicle vitals directly into the operator's line of sight without requiring them to decipher raw hexadecimal logs or launch a browser-based dashboard.
* In `cmd/bench-check`, the terminal screen is cleared at 2 Hz (`\033[H\033[2J`) to render an ANSI cockpit HUD showing real-time roll/pitch/yaw angles, coordinate lock, battery voltage, and armed state.

### Phase 4: Full Airframe Integration & Flight Testing
- **Objective:** Mount the tested avionics deck onto an NDAA-compliant developer frame (Holybro X500 V2).
- **Hardware Cost:** ~$700 (airframe, motors, ESCs, LiPo battery, and DroneCAN Remote ID).
- **Regulatory Compliance:** Equip DroneCAN Remote ID broadcast module and register under FAA DroneZone.
- **Validation:** Execute controlled autonomous flight tests under FAA Part 107 / TRUST guidelines with Starlink Data Mule backhaul synchronization.
