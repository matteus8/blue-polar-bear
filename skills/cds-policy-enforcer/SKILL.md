---
name: cds-policy-enforcer
description: >-
  Enforces zero-trust Cross Domain Solution (CDS) policies, synthetic security tiers,
  cryptographic SHA-256 integrity checks, automated High-to-Low redactions, and Dead
  Letter Queue (DLQ) quarantine audits. Use this skill when modifying CDS policies,
  auditing data flows, investigating quarantined packets, or adding sanitization rules.
---

# CDS Policy Enforcement & Redaction Skill

This skill provides step-by-step operational procedures for the Cross Domain Solution (CDS) Guard daemon in Blue Polar Bear.

## Core Invariants & Synthetic Tiers

All security classifications in this repository must use synthetic designations:
* `TIER-1: PUBLIC` (Simulated Unclassified)
* `TIER-2: RESTRICTED` (Simulated Secret)
* `TIER-3: CRITICAL` (Simulated Top Secret)

> [!CAUTION]
> **OPSEC Rule:** Never use real USG/DoD classification markings in code, tests, logs, or commit messages.

## Operational Procedures

### 1. Zero-Trust Packet Inspection Workflow

Every ingress packet traversing the tactical boundary must undergo three validation gates:
1. **Schema Validation:** Strict JSON unmarshalling against `schema.SecurityEnvelope`.
2. **Cryptographic Integrity:** Recompute SHA-256 hash of payload and verify it matches `header.digest`.
3. **Policy Evaluation:**
   * `TIER-1: PUBLIC`: Pass-through without alteration.
   * `TIER-2: RESTRICTED`: Coarsen coordinates, scrub mission payload, down-tag to Tier-1, re-sign digest.
   * `TIER-3: CRITICAL`: **Fail-Closed**. Immediately reject and route to DLQ.

### 2. High-to-Low Redaction Rules

When processing `TIER-2: RESTRICTED` packets for egress:
* **Coordinate Coarsening:** Round latitude and longitude to 2 decimal places (~1.1 km precision) using `policy.RoundFloat(coord, 2)`.
* **Altitude Masking:** Quantize altitude to 50m intervals (`math.Floor(alt / 50.0) * 50.0`).
* **Payload Scrubbing:** Replace sensitive mission payload keys with `"[REDACTED_BY_CDS]"`.
* **Down-Tagging:** Set header classification to `TIER-1: PUBLIC` and append caveats `["REDACTED_FROM_TIER2", "SANITIZED_BY_CDS"]`.
* **Re-Signing:** Recompute payload SHA-256 digest so downstream recipients can verify integrity.

### 3. Inspecting Quarantined Packets (DLQ)

To inspect quarantined packets from the live CDS Guard:
```bash
# Query recent DLQ records via HTTP API
curl -s http://127.0.0.1:8081/dlq | jq .

# Inspect persistent JSONL audit log
tail -n 20 logs/cds-audit.jsonl | jq .
```

### 4. Running CDS Policy Unit Tests

Always verify policy engine modifications before deployment:
```bash
go test -v ./pkg/policy/... ./cmd/cds-guard/...
```
