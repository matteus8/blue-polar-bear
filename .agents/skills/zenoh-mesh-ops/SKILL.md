---
name: zenoh-mesh-ops
description: >-
  Manages Eclipse Zenoh mesh routing, topic key expressions, router configurations,
  Data Mule buffering, Starlink backhaul, and REST/SSE plugin debugging. Use this
  skill when modifying Zenoh configs, troubleshooting inter-node communication, or
  testing Zenoh pub/sub and query primitives.
---

# Eclipse Zenoh Mesh Operations Skill

This skill provides configuration guidelines and diagnostic workflows for the Eclipse Zenoh protocol layer in Blue Polar Bear.

## Tactical Architecture & Backhaul

1. **Autonomous Edge Mesh (Local RF):** Drones publish telemetry over local tactical RF / Wi-Fi to a local Zenoh router.
2. **Tactical Data Mule (Field GCS):** Buffers, stores, and validates packets in disconnected DDIL conditions.
3. **Starlink Satellite Backhaul:** Replicates sanitized packets across satellite backhaul to the cloud relay (`relay.platformstaq.com`).
4. **Hybrid Ingestion:** C2 Gateway translates Zenoh streams to standard WebSockets and REST APIs for web browsers.

## Topic Key Expression Standard

All Zenoh pub/sub and query keys must follow the 6-token structured format:
```text
sec/<synthetic_tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>
```

### Valid Key Examples:
* `sec/tier2/drone/blue/blue-alpha/telemetry` (Tactical ingress)
* `sec/tier1/drone/blue/blue-alpha/telemetry` (Sanitized egress)
* `sec/tier2/drone/red/red-1/telemetry` (Adversary track)
* `sec/tier2/drone/blue/blue-alpha/command` (C2 flight instruction)

## Operational Procedures

### 1. Running the Local Tactical Router

Start the GCS router locally using the project configuration:
```bash
zenohd --config configs/zenoh-gcs.json5
```
This starts:
* Zenoh Mesh TCP on `tcp/0.0.0.0:7447`
* Zenoh REST & SSE Plugin on `http://0.0.0.0:8000`

### 2. Querying Telemetry via Zenoh REST Plugin

Query all active telemetry matching a wildcard selector:
```bash
# Query all sanitized tier1 telemetry
curl -s "http://127.0.0.1:8000/sec/tier1/**" | jq .

# Query specific drone state
curl -s "http://127.0.0.1:8000/sec/tier1/drone/blue/blue-alpha/telemetry" | jq .
```

### 3. Streaming Real-Time Telemetry via SSE

Subscribe to live telemetry streams using Server-Sent Events (SSE):
```bash
curl -N -H "Accept: text/event-stream" "http://127.0.0.1:8000/sec/**"
```

### 4. Zenoh Admin Space Inspection

Inspect active storages, routers, and connected peers:
```bash
curl -s -g "http://127.0.0.1:8000/@/**" | jq .
```
