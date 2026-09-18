---
name: zero-trust-testing
description: >-
  Executes automated verification suites, zero-trust integrity checks, regression tests,
  and container integration playbooks for Blue Polar Bear. Use this skill before
  submitting commits, validating new features, or verifying system health.
---

# Zero-Trust Verification & Testing Playbook

This skill outlines the standard testing procedures required before completing tasks or merging pull requests.

## Testing Playbook

### 1. Execute All Go Unit & Integration Tests

Run the complete Go test suite covering schemas, policies, Zenoh utilities, CDS guard, and C2 gateway:
```bash
go test -v ./...
```

To run with coverage profiling:
```bash
go test -v -cover ./...
```

### 2. Verify Python Syntax & Test Suites

Verify syntax compliance and run the Python edge agent test suite:
```bash
# Syntax check
python3 -m py_compile cmd/edge-agent-py/*.py

# Unit tests
.venv/bin/python3 cmd/edge-agent-py/test_agent.py
```

### 3. Verify Production Binary Compilation

Ensure all Go daemons compile without linker or dependency issues:
```bash
go build -o /dev/null ./cmd/cds-guard
go build -o /dev/null ./cmd/c2-gateway
go build -o /dev/null ./cmd/edge-agent
```

### 4. Full Stack Container Integration Smoke Test

Launch the complete multi-container stack and verify inter-service communication:
```bash
# Build and launch containers
docker compose up -d --build

# Verify container health
docker compose ps

# Check CDS Guard inspection API
curl -s http://localhost:8081/health | jq .

# Check C2 Gateway fleet telemetry
curl -s http://localhost:8080/api/v1/fleet | jq .

# Check Zenoh router REST interface
curl -s http://localhost:8000/sec/** | jq .

# Teardown when testing completes
docker compose down
```

### 5. Specific Zero-Trust Invariant Checks

* **Integrity Tamper Test:** Modifying any payload byte without updating `header.digest` MUST fail envelope validation with `cryptographic digest mismatch`.
* **TIER-3 Egress Test:** Any packet with `TIER-3: CRITICAL` reaching the CDS Guard MUST be routed to the DLQ and rejected from egress.
* **Redaction Test:** Any `TIER-2: RESTRICTED` packet MUST emerge down-tagged to `TIER-1: PUBLIC` with coordinates coarsened to 2 decimal places.
