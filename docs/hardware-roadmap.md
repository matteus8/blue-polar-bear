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
- **Hardware Cost:** ~$180–$210.
- **Safety Precaution:** Propellers completely removed; unit operated on desk.
- **Validation:** Verify real physical IMU/accelerometer orientation and MAVLink C2 command response.

### Phase 3: Companion Single-Board Computer (Raspberry Pi 5 ARM64)
- **Objective:** Cross-compile `edge-agent` for Linux ARM64 (`GOOS=linux GOARCH=arm64`).
- **Hardware Cost:** ~$90 (Pi 5 + USB-C / UART wiring).
- **Interconnect:** Wire the Pi 5 to the Pixhawk's `TELEM2` port over 4-pin UART serial.
- **Validation:** Validate peer-to-peer Zenoh mesh connectivity over Wi-Fi / tactical RF to the GCS laptop.

### Phase 4: Full Airframe Integration & Flight Testing
- **Objective:** Mount the tested avionics deck onto an NDAA-compliant developer frame (Holybro X500 V2).
- **Hardware Cost:** ~$700 (airframe, motors, ESCs, LiPo battery, and DroneCAN Remote ID).
- **Regulatory Compliance:** Equip DroneCAN Remote ID broadcast module and register under FAA DroneZone.
- **Validation:** Execute controlled autonomous flight tests under FAA Part 107 / TRUST guidelines with Starlink Data Mule backhaul synchronization.
