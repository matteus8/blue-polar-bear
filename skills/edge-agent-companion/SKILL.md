---
name: edge-agent-companion
description: >-
  Builds, runs, and tunes vehicle companion agents for ARM64 hardware (Raspberry Pi 5)
  and simulated flight dynamics in Go and Python. Use this skill when deploying edge
  agents, adjusting flight physics, testing battery models, or configuring physical
  vehicle companion computers.
---

# Edge Vehicle Companion Agent Skill

This skill provides operational and development procedures for edge companion agents simulating physical drones and rovers.

## Available Agent Implementations

1. **Go Companion Agent (`cmd/edge-agent/`):** High-concurrency, low-latency telemetry generator with orbital waypoint simulation and multi-tier emission.
2. **Python Companion Agent (`cmd/edge-agent-py/`):** Dedicated companion script for Raspberry Pi 5 hardware running Python 3.10+ and the `eclipse-zenoh` SDK.

## Operational Procedures

### 1. Running the Go Edge Simulator

Run Drone Bravo with 1 Hz telemetry emission:
```bash
# Connect to local Zenoh router REST interface:
go run cmd/edge-agent/main.go -id bravo -type drone -team blue -rate 1.0

# Run in standalone mock mode (no router required):
go run cmd/edge-agent/main.go -id bravo -mock -rate 1.0

# Enable periodic TIER-3 Sovereign packet emission (to test CDS fail-closed quarantine):
go run cmd/edge-agent/main.go -id bravo -emit-tier3=true
```

### 2. Running the Python Pi 5 Companion Agent

Run the Python companion computer agent using the project virtual environment:
```bash
# Activate environment & run with default config:
.venv/bin/python3 cmd/edge-agent-py/main.py --id alpha --type drone --team blue --rate 1.0

# Point to specific Zenoh edge config:
.venv/bin/python3 cmd/edge-agent-py/main.py --id alpha --config configs/zenoh-edge.json5
```

### 3. Deploying to Physical Raspberry Pi 5 (ARM64)

To cross-compile the Go edge agent for physical ARM64 hardware:
```bash
GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o bin/edge-agent-arm64 ./cmd/edge-agent
```
Deploy the binary or run Python on the Pi:
```bash
scp bin/edge-agent-arm64 pi@pi-ip:~/
scp configs/zenoh-edge.json5 pi@pi-ip:~/
```

### 4. Simulating C2 Flight Modes

Both agents implement an internal flight state machine:
* `AIRBORNE` / `PATROL`: Circular orbit around base coordinates with battery consumption of 0.04-0.05% per second.
* `HOVER`: Stationary loiter holding position with reduced battery burn.
* `RTB`: Descent at -2 m/s until reaching ground elevation, then transitions to `LANDED`.
* `LANDED`: Propulsion disarmed, awaiting `ARM` instruction.
