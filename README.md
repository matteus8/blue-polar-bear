# Blue Polar Bear: Tactical Edge-to-Cloud C2 & Telemetry Mesh

[![Release](https://img.shields.io/badge/Release-v0.6.0-blue.svg)](https://github.com/matteus8/blue-polar-bear/releases/tag/v0.6.0)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Protocol: Eclipse Zenoh](https://img.shields.io/badge/Protocol-Eclipse_Zenoh_1.1.0-orange.svg)](https://zenoh.io/)
[![Stack: Go](https://img.shields.io/badge/Language-Go_1.22+-00ADD8.svg)](https://go.dev/)
[![Tests: 100% Race Clean](https://img.shields.io/badge/Tests-100%25_Race_Clean-success.svg)](docs/development.md)

---

## Overview

**Blue Polar Bear** is a distributed, zero-trust Tactical Command & Control (C2) and Telemetry system built in Go. Designed for edge robotics in Disconnected, Degraded, Intermittent, and Latent (DDIL) environments, it enables autonomous drone swarms to communicate over peer-to-peer **Eclipse Zenoh** mesh networks without relying on continuous cloud connectivity.

The platform bridges tactical RF communications to modern web dashboards. It combines automated **zero-trust cross-domain data sanitization**, an offline **tactical data mule** that buffers packets during satellite blackouts, and real-time **WebSocket streaming** to an operator Common Operating Picture (COP).

---

## Repository Tour

Explore the core components of the system:

* [`cmd/`](cmd/) — Production Go microservices:
  * [`cmd/edge-agent`](cmd/edge-agent/) — 10-drone swarm telemetry simulator (5 Blue friendly, 5 Red adversary) with physics and C2 listeners.
  * [`cmd/cds-guard`](cmd/cds-guard/) — Zero-trust Cross Domain Solution guard with fail-closed egress, coordinate coarsening, and Dead Letter Queue auditing.
  * [`cmd/c2-gateway`](cmd/c2-gateway/) — Ingress bridge translating Zenoh mesh topics into standard WebSockets and REST APIs.
* [`pkg/`](pkg/) — Core domain logic and shared libraries:
  * [`pkg/mavlink`](pkg/mavlink/) — Pure Go MAVLink v2 protocol codec, UDP client, and SITL autopilot bridge.
  * [`pkg/schema`](pkg/schema/) — Canonical data envelopes, SHA-256 integrity verification, and command types.
  * [`pkg/policy`](pkg/policy/) — Security tiers, coordinate sanitization rules, and DLQ audit logging.
  * [`pkg/zenohutil`](pkg/zenohutil/) — Tactical Data Mule disk spooler, Zenoh session helpers, and REST client.
* [`web/`](web/) — Browser-based Tactical COP dashboard featuring real-time radar sweeps, Leaflet maps, and flight command dispatch.
* [`deploy/`](deploy/) & [`configs/`](configs/) — Container build specifications and multi-tier Zenoh mesh network configurations.

---

## Documentation & Deep Dives

* 📖 **[System Architecture & Data Flows](docs/architecture.md)** — Hybrid mesh topology, end-to-end data flows, Starlink DDIL mechanics, and synthetic classification tiers.
* 🛠️ **[Developer & Operations Guide](docs/development.md)** — Quickstart execution (Docker Compose / native Go), Makefile commands, and automated test suite.
* ✈️ **[Physical Hardware & Autonomy Roadmap](docs/hardware-roadmap.md)** — 4-phase benchtop-to-flight progression from PX4 SITL to physical Pixhawk and Holybro airframe flight tests.
* 🤖 **[Agent Standards & Operating Rules](AGENTS.md)** — Architectural invariants, OPSEC guidelines, and multi-agent coordination protocols.

---

## License

Licensed under the [Apache License, Version 2.0](https://opensource.org/licenses/Apache-2.0).
