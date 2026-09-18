---
name: tactical-c2-dispatch
description: >-
  Operates the Tactical Command & Control (C2) Gateway, WebSocket streaming bridge,
  flight command dispatcher, and browser-based Common Operating Picture (COP). Use
  this skill when testing C2 flight instructions, troubleshooting WebSocket telemetry,
  or enhancing the dashboard interface.
---

# Tactical C2 Dispatch & COP Operations Skill

This skill outlines operational procedures for the Tactical Ground Control Station (GCS) C2 Gateway and Common Operating Picture (COP) dashboard.

## Gateway Capabilities

* **WebSocket Hub (`/ws/telemetry`):** Streams sanitized egress telemetry envelopes to connected operator dashboards.
* **C2 Command API (`POST /api/v1/command`):** Signs, validates, and dispatches flight instructions to edge vehicles over Zenoh.
* **Fleet State Tracker (`GET /api/v1/fleet`):** Returns the latest telemetry and breadcrumb trail for all active assets.
* **Static COP Server (`/`):** Serves the HTML5/Canvas/Leaflet tactical operator console.

## Operational Procedures

### 1. Starting the C2 Gateway

Run the gateway pointing to the local Zenoh router or with the in-memory mock bus:
```bash
# Connected to Zenoh REST router:
go run cmd/c2-gateway/main.go -router http://127.0.0.1:8000 -port 8080

# Standalone mock mode:
go run cmd/c2-gateway/main.go -mock -port 8080
```

### 2. Dispatching C2 Flight Commands

Commands can be dispatched via HTTP POST to `/api/v1/command`:

#### Return to Base (RTB):
```bash
curl -X POST http://127.0.0.1:8080/api/v1/command \
  -H "Content-Type: application/json" \
  -d '{
    "target_vehicle": "bravo",
    "command_type": "RETURN_TO_BASE",
    "parameters": {"reason": "Low Battery / Mission Complete"}
  }'
```

#### Loiter / Hover:
```bash
curl -X POST http://127.0.0.1:8080/api/v1/command \
  -H "Content-Type: application/json" \
  -d '{
    "target_vehicle": "bravo",
    "command_type": "HOVER",
    "parameters": {"altitude_m": 120.0}
  }'
```

#### Resume Patrol:
```bash
curl -X POST http://127.0.0.1:8080/api/v1/command \
  -H "Content-Type: application/json" \
  -d '{
    "target_vehicle": "bravo",
    "command_type": "PATROL",
    "parameters": {"pattern": "ORBIT"}
  }'
```

### 3. Monitoring Fleet Health & Tracks

Inspect current active vehicle positions and breadcrumb tracks:
```bash
curl -s http://127.0.0.1:8080/api/v1/fleet | jq .
```

### 4. WebSocket Telemetry Verification

Verify WebSocket broadcast using `wscat` or websocat:
```bash
websocat ws://127.0.0.1:8080/ws/telemetry
```
Ensure incoming envelopes include `header` and `telemetry` JSON objects.
