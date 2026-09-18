# AGENTS.md: Autonomous Agent Operating Procedures & Standards

Welcome, AI Agent. You are contributing to **Blue Polar Bear**, a distributed, zero-trust Tactical Command & Control (C2) and Telemetry system built with Eclipse Zenoh, Go, and Python.

This document defines the persistent instructions, architectural invariants, code standards, and multi-agent coordination protocols for this repository.

---

## 1. Core Architectural Invariants

1. **Protocol Layer:** All inter-node communication uses **Eclipse Zenoh**. Do NOT introduce MQTT, Kafka, or heavy brokers unless explicitly instructed.
2. **Topic Key Standard:** Key expressions MUST follow the structured format:
   ```text
   sec/<synthetic_tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>
   # Example: sec/tier2/drone/blue/bravo/telemetry
   ```
3. **OPSEC & Classification Compliance:** 
   - **NEVER** use real-world USG/DoD classification strings (e.g. "SECRET", "TOP SECRET") in code, tests, logs, or commit messages.
   - **ALWAYS** use the synthetic tiers:
     - `TIER-1: PUBLIC` (Simulated Unclass)
     - `TIER-2: RESTRICTED` (Simulated Secret)
     - `TIER-3: CRITICAL` (Simulated Top Secret)
4. **Zero-Trust Cross Domain Solution (CDS):**
   - The CDS Guard must be **Fail-Closed**. Any unparseable, malformed, or policy-violating packet must be quarantined to a Dead Letter Queue (DLQ) and generate an immutable audit log.
   - `TIER-3: CRITICAL` packets must NEVER egress beyond the local tactical boundary.

---

## 2. Language & Engineering Standards

### Go (`cmd/cds-guard`, `cmd/c2-gateway`, `cmd/edge-agent`)
- **Version:** Go 1.22+
- **Idiomatic Style:** Strict compliance with `gofmt` and `golangci-lint`.
- **Error Handling:** Never swallow errors. Always wrap or log errors with contextual detail (`fmt.Errorf("validating schema: %w", err)`). Never use `panic()` in production paths.
- **Concurrency:** Goroutines must be managed with `context.Context` and `sync.WaitGroup` for graceful shutdown.

### Python (`cmd/edge-agent-py`)
- **Version:** Python 3.10+
- **Style:** PEP 8 compliance, `snake_case` for variables and functions.
- **Typing:** Use Python type annotations (`typing.Dict`, `typing.Optional`, dataclasses/pydantic).
- **Logging:** Use `logging.getLogger(__name__)` instead of bare `print()` statements for operational code.

---

## 3. Directory Layout & Module Responsibilities

```text
edgeCompute/
├── cmd/
│   ├── edge-agent/        # Telemetry generator simulating physical vehicle (Go)
│   ├── edge-agent-py/     # Alternative Python edge agent (Pi 5 companion)
│   ├── cds-guard/         # Cross Domain Solution guard & redaction daemon (Go)
│   └── c2-gateway/        # WebSocket/HTTP server streaming telemetry to web (Go)
├── pkg/
│   ├── schema/            # Canonical data structs (SecurityHeader, TelemetryPayload)
│   ├── policy/            # CDS redaction and security tier rules
│   └── zenohutil/         # Reusable Zenoh session, config, and retry helpers
├── web/                   # Web-based Tactical C2 Dashboard (HTML5/JS/Canvas)
├── configs/               # Zenoh JSON5 configs for Edge, GCS, and Cloud Router
└── archive/               # Preserved legacy scripts and exploratory code
```

---

## 4. Multi-Agent Task Allocation Matrix

When collaborating with other agents or handling subtasks:

| Role Name | Scope of Work | Primary Files |
| :--- | :--- | :--- |
| **`Agent-DataModel`** | Defines canonical schemas, serialization (JSON/Protobuf), and policy types. | `pkg/schema/`, `pkg/policy/` |
| **`Agent-Edge`** | Implements realistic vehicle telemetry (GPS, battery, state machine). | `cmd/edge-agent/`, `configs/zenoh-edge.json5` |
| **`Agent-CDS`** | Implements the Zero-Trust CDS Guard (schema validation, redaction, DLQ). | `cmd/cds-guard/`, `pkg/policy/` |
| **`Agent-C2`** | Implements the GCS gateway, WebSocket bridge, and Tactical COP UI. | `cmd/c2-gateway/`, `web/` |
| **`Agent-Infra`** | Cloudflare tunnels, Dockerfiles, and `docker-compose.yml`. | `deploy/`, `Dockerfile*`, `compose.yml` |

---

## 5. Verification & Testing Playbook

Before marking any task complete, run the relevant verification steps:

```bash
# 1. Verify Go syntax and test coverage
go test -v ./...

# 2. Verify Python syntax
python3 -m py_compile cmd/edge-agent-py/*.py

# 3. Test Zenoh Session Connectivity (Local Mock)
# Start local router:
zenohd --config configs/zenoh-gcs.json5
```

---

## 6. Commit & PR Guidelines

- Follow conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `sec:`.
- Ensure all legacy code remains safely in `archive/`.
- Ensure `.gitignore` prevents virtual environments and binaries from being committed.
