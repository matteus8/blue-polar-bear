---
name: edge-agent-companion
description: >-
  Builds, runs, and tunes the 10-drone tactical swarm simulator in Go (5 Blue friendly,
  5 Red adversary). Use this skill when deploying edge agents, adjusting orbital flight
  dynamics, testing battery models, or simulating C2 command responses.
---

# Edge Vehicle Companion & Swarm Simulator Skill

This skill provides operational and development procedures for the multi-vehicle swarm simulator in Blue Polar Bear.

## Swarm Architecture (10 Drones)

The Go edge simulator (`cmd/edge-agent/`) manages concurrent autonomous drone instances using lightweight goroutines:

* **Blue Fleet (5 Friendly Drones):**
  - Callsigns: `blue-alpha`, `blue-bravo`, `blue-charlie`, `blue-delta`, `blue-echo`
  - Base coordinates: Friendly western sector (`37.7700, -122.4300`)
  - Subscribes to C2 flight instructions on `sec/tier2/drone/blue/<id>/command`.
* **Red Fleet (5 Adversary Drones):**
  - Callsigns: `red-1`, `red-2`, `red-3`, `red-4`, `red-5`
  - Base coordinates: Adversary eastern sector (`37.7950, -122.3950`)
  - Executes autonomous adversary patrol loops.

## Operational Procedures

### 1. Running the Full 10-Drone Swarm

Launch all 10 drones (5 Blue, 5 Red) pointing to the local Zenoh router:
```bash
go run cmd/edge-agent/main.go -swarm -blue-count 5 -red-count 5 -router http://127.0.0.1:8000 -rate 1.0
```

To run in standalone mock mode without a router daemon:
```bash
go run cmd/edge-agent/main.go -swarm -blue-count 5 -red-count 5 -mock
```

### 2. Running a Single Vehicle for Isolated Testing

Run an individual vehicle:
```bash
go run cmd/edge-agent/main.go -id bravo -team blue -rate 1.0 -mock
```

### 3. Emitting Periodic TIER-3 Sovereign Payloads

Enable periodic `TIER-3: CRITICAL` emission (every 15th packet on drone 1) to verify CDS fail-closed quarantine:
```bash
go run cmd/edge-agent/main.go -swarm -emit-tier3=true
```

### 4. Simulating C2 Flight Modes

Each drone implements an internal flight state machine:
* `AIRBORNE` / `PATROL`: Circular orbit around base coordinates with battery consumption of 0.04% per second.
* `HOVER`: Stationary loiter holding position with reduced battery burn.
* `RTB`: Controlled descent at -2 m/s until reaching ground elevation, then transitions to `LANDED`.
* `LANDED`: Propulsion disarmed, awaiting `ARM` instruction.
