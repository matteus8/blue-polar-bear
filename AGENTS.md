# AGENTS.md: Autonomous Agent Operating Procedures & Standards

Welcome, AI Agent. You are contributing to **Blue Polar Bear**, a distributed, zero-trust Tactical Command & Control (C2) and Telemetry system built with Eclipse Zenoh, Go, and WebSockets.

This document defines the persistent instructions, architectural invariants, code standards, and multi-agent coordination protocols for this repository.

---

## 1. Core Architectural Invariants

1. **Protocol & Mesh Layer:**
   - All inter-node mesh communication uses **Eclipse Zenoh**.
   - Edge drones publish telemetry over local tactical RF / Wi-Fi to a local Zenoh router.
   - The Field GCS acts as a **Tactical Data Mule**, storing and forwarding packets in DDIL (Disconnected, Degraded, Intermittent, Latent) environments.
   - When backhaul connectivity is restored via **Starlink** (or BLOS satellite link), Zenoh synchronizes sanitized data upstream to the cloud router (`relay.platformstaq.com`).
2. **Hybrid Ingestion Standard:**
   - Zenoh is the distributed transport backbone across Edge, Data Mule, and Cloud Relay.
   - The **C2 Gateway (`cmd/c2-gateway`)** bridges Zenoh telemetry into standard **WebSockets (`/ws/telemetry`)** and **REST APIs (`/api/v1/fleet`, `/api/v1/command`)** for browser dashboards (COP) and enterprise clients.
3. **Topic Key Standard:** Key expressions MUST follow the structured 6-token format:
   ```text
   sec/<synthetic_tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>
   # Examples:
   # sec/tier2/drone/blue/bravo/telemetry
   # sec/tier1/drone/blue/bravo/telemetry
   # sec/tier2/drone/blue/bravo/command
   ```
4. **OPSEC & Classification Compliance:** 
   - **NEVER** use real-world USG/DoD classification strings (e.g. "SECRET", "TOP SECRET") in code, tests, logs, or commit messages.
   - **ALWAYS** use the synthetic tiers:
     - `TIER-1: PUBLIC` (Simulated Unclass)
     - `TIER-2: RESTRICTED` (Simulated Secret)
     - `TIER-3: CRITICAL` (Simulated Top Secret)
5. **Zero-Trust Cross Domain Solution (CDS):**
   - The CDS Guard must be **Fail-Closed**. Any unparseable, malformed, or policy-violating packet must be quarantined to a Dead Letter Queue (DLQ) with an immutable JSONL audit log.
   - `TIER-3: CRITICAL` packets must NEVER egress beyond the local tactical boundary.
   - `TIER-2: RESTRICTED` packets must be coarsened (GPS rounded to 2 decimal places ~1.1km) and sanitized before being down-tagged to `TIER-1: PUBLIC`.
6. **Fleet Scalability & Zero-Bloat Edge Invariant (10 to 1,000+ Drones):**
   - **Zero Edge Bloat:** Software must remain strictly modular, lightweight, and zero-bloat. Never introduce heavyweight runtimes (Node.js, Python, Java) onto edge airframes.
   - **Pure Go & Multi-Arch Matrix:** All edge binaries must remain 100% pure Go (`CGO_ENABLED=0`) capable of compiling to `linux/arm64` (Raspberry Pi 5 / NVIDIA Jetson) and `linux/amd64` via automated CI matrix without CGo toolchains.
   - **Stateless Configuration:** Edge agents must be dynamically configurable via CLI flags (`-id`, `-team`, `-mavlink-addr`, `-rate`) or environment variables—never hardcoded arrays or callsign switches.
   - **Decentralized O(1) Routing:** Inter-node pub/sub overhead must scale $O(1)$ per edge drone using peer-to-peer Eclipse Zenoh mesh routing without central database or message broker bottlenecks. Flashing and provisioning hundreds of airframes must require zero codebase rework.

---

## 2. Language & Engineering Standards

### Go (`cmd/cds-guard`, `cmd/c2-gateway`, `cmd/edge-agent`, `pkg/*`)
- **Version:** Go 1.22+
- **Idiomatic Style:** Strict compliance with `gofmt` and `golangci-lint`.
- **Error Handling:** Never swallow errors. Always wrap or log errors with contextual detail (`fmt.Errorf("validating schema: %w", err)`). Never use `panic()` in production paths.
- **Concurrency:** Goroutines must be managed with `context.Context` and `sync.WaitGroup` for graceful shutdown.

---

## 3. Directory Layout & Module Responsibilities

```text
edgeCompute/
├── .agents/skills/        # Workspace agent runbooks & skill procedures
├── cmd/
│   ├── edge-agent/        # 10-drone swarm telemetry simulator (5 Blue, 5 Red) (Go)
│   ├── cds-guard/         # Cross Domain Solution guard & redaction daemon (Go)
│   └── c2-gateway/        # WebSocket/HTTP server streaming telemetry to web (Go)
├── pkg/
│   ├── schema/            # Canonical data structs (SecurityHeader, TelemetryPayload)
│   ├── policy/            # CDS redaction, synthetic tiers, and DLQ audit log
│   └── zenohutil/         # Reusable Zenoh session, topic standards, and REST client
├── web/                   # Web-based Tactical C2 Dashboard (HTML5/Canvas/Leaflet)
├── configs/               # Zenoh JSON5 configs for Edge, GCS, and Cloud Router
├── deploy/                # Multi-stage Dockerfiles for containerized services
├── compose.yml            # Multi-container tactical mesh orchestration
├── go.mod                 # Go module definitions
└── AGENTS.md              # Multi-agent operating procedures & development rules
```

---

## 4. Multi-Agent Task Allocation Matrix

When collaborating with other agents or handling subtasks:

| Role Name | Scope of Work | Primary Files |
| :--- | :--- | :--- |
| **`Agent-DataModel`** | Defines canonical schemas, SHA-256 serialization, and policy types. | `pkg/schema/`, `pkg/policy/` |
| **`Agent-Edge`** | Implements 10-drone swarm flight physics, battery models, and C2 listeners. | `cmd/edge-agent/`, `configs/zenoh-edge.json5` |
| **`Agent-CDS`** | Implements the Zero-Trust CDS Guard (schema validation, redaction, DLQ). | `cmd/cds-guard/`, `pkg/policy/` |
| **`Agent-C2`** | Implements GCS gateway, WebSocket bridge, REST APIs, and Tactical COP UI. | `cmd/c2-gateway/`, `web/` |
| **`Agent-Infra`** | Starlink backhaul, Cloudflare tunnels, Dockerfiles, and `compose.yml`. | `deploy/`, `configs/`, `compose.yml` |

---

## 5. Swarm Operational Specifications (10 Drones)

The system simulates an asymmetric tactical engagement:
* **Blue Fleet (5 Friendly Drones):**
  - Callsigns: `blue-alpha`, `blue-bravo`, `blue-charlie`, `blue-delta`, `blue-echo`
  - Clustered in friendly western operating sector.
  - Fully controllable via operator C2 instructions (`RETURN_TO_BASE`, `HOVER`, `PATROL`, `ARM`, `DISARM`).
* **Red Fleet (5 Adversary Drones):**
  - Callsigns: `red-1`, `red-2`, `red-3`, `red-4`, `red-5`
  - Clustered in adversary eastern operating sector.
  - Follow autonomous patrol trajectories and emit adversary sensor telemetry.

---

## 6. Verification & Testing Playbook

Before marking any task complete, run the relevant verification steps:

```bash
# 1. Verify Go syntax and test coverage
go test -v ./...

# 2. Verify all Go production binaries compile
go build -o /dev/null ./cmd/cds-guard
go build -o /dev/null ./cmd/c2-gateway
go build -o /dev/null ./cmd/edge-agent

# 3. Test multi-container stack integration
docker compose up -d --build
docker compose ps
docker compose down
```

---

## 7. Commit & PR Guidelines

- Follow conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `sec:`.
- Ensure `.gitignore` prevents virtual environments and binaries from being committed.

---

## 8. Physical Hardware & Autonomy Integration Roadmap

When transitioning from swarm simulation to physical hardware, agents must adhere to the 4-phase benchtop-to-flight progression:

1. **Phase 1: Virtual Avionics (PX4 SITL on UDP `14550`)**
   - Connect `cmd/edge-agent` to the open-source PX4 Autopilot software simulator via MAVLink (`pkg/mavlink`).
   - Validate that virtual GPS, battery, and attitude stream into `schema.SecurityEnvelope` without physical hardware.
2. **Phase 2: Benchtop Flight Controller (Desk Hardware-in-the-Loop)**
   - Connect `cmd/edge-agent` to a physical Pixhawk 6C / Cube flight controller over USB serial (`/dev/tty.usbmodem1` on macOS or `/dev/ttyACM0` on Linux) at 115200/921600 baud.
   - Verify real physical IMU/accelerometer orientation and MAVLink command response on the bench with propellers removed.
3. **Phase 3: Companion Single-Board Computer (Raspberry Pi 5 ARM64)**
   - Cross-compile `edge-agent` for Linux ARM64 (`GOOS=linux GOARCH=arm64`).
   - Wire the Pi 5 to the Pixhawk's `TELEM2` port over 4-pin UART serial.
   - Validate peer-to-peer Zenoh mesh connectivity over Wi-Fi/tactical RF to the GCS laptop.
4. **Phase 4: Full Airframe Integration & Flight Testing**
   - Mount the tested avionics deck onto an NDAA-compliant developer frame (Holybro X500 V2).
   - Equip DroneCAN Remote ID broadcast module and register under FAA DroneZone.
   - Execute controlled autonomous flight tests under FAA Part 107 / TRUST guidelines.

