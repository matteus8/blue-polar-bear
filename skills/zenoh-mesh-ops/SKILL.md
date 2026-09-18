---
name: zenoh-mesh-ops
description: >-
  Manages Eclipse Zenoh mesh routing, topic key expressions, router configurations,
  REST/SSE plugin debugging, and connectivity between Edge, GCS, and Cloud nodes.
  Use this skill when modifying Zenoh configs, troubleshooting inter-node communication,
  or testing Zenoh pub/sub and query primitives.
---

# Eclipse Zenoh Mesh Operations Skill

This skill provides configuration guidelines and diagnostic workflows for the Eclipse Zenoh protocol layer in Blue Polar Bear.

## Topic Key Expression Standard

All Zenoh pub/sub and query keys must follow the 6-token structured format:
```text
sec/<synthetic_tier>/<vehicle_type>/<team>/<unit_id>/<stream_type>
```

### Valid Key Examples:
* `sec/tier2/drone/blue/bravo/telemetry` (Tactical ingress)
* `sec/tier1/drone/blue/bravo/telemetry` (Sanitized egress)
* `sec/tier3/drone/blue/bravo/telemetry` (Sovereign / EW test)
* `sec/tier2/drone/blue/bravo/command` (C2 flight instruction)

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
curl -s "http://127.0.0.1:8000/sec/tier1/drone/blue/bravo/telemetry" | jq .
```

### 3. Publishing Test Payloads via Zenoh REST

Publish a simulated telemetry envelope using HTTP PUT:
```bash
curl -X PUT \
  -H "Content-Type: application/json" \
  -d '{"header":{"classification":"TIER-1: PUBLIC","origin_enclave":"test-gcs","timestamp_ns":1789751633000000000,"message_id":"msg-test","digest":"abc"},"telemetry":{"vehicle_id":"test-01","vehicle_type":"drone","team":"blue","state":"AIRBORNE","battery_pct":90.0,"coordinates":{"lat":37.77,"lon":-122.42,"alt_m":100},"velocity":{"speed_mps":10},"sequence":1}}' \
  "http://127.0.0.1:8000/sec/tier1/drone/blue/test-01/telemetry"
```

### 4. Streaming Real-Time Telemetry via SSE

Subscribe to live telemetry streams using Server-Sent Events (SSE):
```bash
curl -N -H "Accept: text/event-stream" "http://127.0.0.1:8000/sec/**"
```

### 5. Zenoh Admin Space Inspection

Inspect active storages, routers, and connected peers:
```bash
curl -s -g "http://127.0.0.1:8000/@/**" | jq .
```
