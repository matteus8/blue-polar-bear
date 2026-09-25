# Blue Polar Bear: Developer & Operations Guide

This guide covers building, running, testing, and debugging the **Blue Polar Bear** distributed tactical mesh stack.

---

## 1. Quickstart Execution Guide

### Option A: Local Multi-Container Stack (Recommended)

Launch the Zenoh router, CDS Guard, C2 Gateway, and 10-drone swarm:

```bash
make up
```

- **Tactical COP Dashboard:** [http://localhost:8080](http://localhost:8080)
- **CDS Guard Inspection API:** [http://localhost:8081/health](http://localhost:8081/health) and [http://localhost:8081/dlq](http://localhost:8081/dlq)
- **Zenoh REST Interface:** [http://localhost:8000](http://localhost:8000)
- **Backhaul Status:** [http://localhost:8080/api/v1/backhaul](http://localhost:8080/api/v1/backhaul)

To shut down the containerized stack:
```bash
make down
```

---

### Option B: Native Host Execution (Standalone Mock Bus)

Each component supports in-memory standalone mode without requiring a local Zenoh daemon:

1. **Start the Cross Domain Solution Guard:**
   ```bash
   go run cmd/cds-guard/main.go -mock -http-port 8081
   ```

2. **Start the Tactical C2 Gateway & Web COP:**
   ```bash
   go run cmd/c2-gateway/main.go -mock -port 8080
   ```
   Access the dashboard at `http://127.0.0.1:8080`.

3. **Start the 10-Drone Swarm Simulator (5 Blue, 5 Red):**
   ```bash
   go run cmd/edge-agent/main.go -swarm -blue-count 5 -red-count 5 -mock
   ```

4. **Run Desk HITL Avionics Sniffer & Terminal HUD (`bench-check`):**
   ```bash
   # Sniff physical Pixhawk connected via USB or companion UART
   go run cmd/bench-check/main.go -serial /dev/ttyACM0 -baud 115200

   # Sniff simulated PX4 SITL UDP packets
   go run cmd/bench-check/main.go -udp :14550
   ```

---

## 2. Developer Tooling & Makefile Targets

A unified [`Makefile`](file:///Users/mcamacho/git-repos/edgeCompute/Makefile) provides automated shortcuts for common tasks:

| Target | Description | Command |
| :--- | :--- | :--- |
| `make test` | Run all Go unit and integration tests | `go test -count=1 -v ./...` |
| `make test-race` | Run all tests with Go race detector enabled | `go test -count=1 -v -race ./...` |
| `make build` | Compile all 4 service and sniffer binaries into `./bin` | Builds `cds-guard`, `c2-gateway`, `edge-agent`, `bench-check` |
| `make cross-build` | Multi-arch static build (`linux/arm64`, `linux/amd64`) | Builds stripped binaries for drone fleet flashing |
| `make lint` | Check formatting compliance | `gofmt -s -l .` |
| `make fmt` | Automatically format all Go source code | `gofmt -s -w .` |
| `make up` | Build & launch multi-container mesh in background | `docker compose up -d --build` |
| `make down` | Tear down mesh containers and networks | `docker compose down` |
| `make logs` | Tail live logs across all containers | `docker compose logs -f` |
| `make status` | Check status of running containers | `docker compose ps` |
| `make clean` | Remove binaries (`bin/`) and spool logs (`logs/`) | `rm -rf bin logs/*.jsonl` |

---

## 3. Verification & Automated Testing Playbook

The repository enforces 100% race-free test coverage across all microservices and packages:

```bash
make test-race
```

### Key Test Coverage Areas
* **Cryptographic Integrity & Tamper Detection:** Verifies that any modified bit in a payload causes immediate SHA-256 digest validation failure and quarantining to the DLQ.
* **Geofence & Battery Guardrails:** Enforces bounds on latitude `[-90, 90]`, longitude `[-180, 180]`, and battery levels `[0, 100]`.
* **Zero-Trust Egress Policy:** Asserts that `TIER-3: CRITICAL` packets are blocked (fail-closed) from crossing the tactical boundary, while `TIER-2: RESTRICTED` packets are coarsened and down-tagged to `TIER-1: PUBLIC`.
* **Tactical Data Mule DDIL Lifecycle:** Verifies disk spooling during simulated Starlink disconnects and chronological backlog flushing upon reconnection.
* **WebSocket Streaming & C2 Command Dispatch:** Tests real-time WebSocket client broadcast and Zenoh command publication with proper tier classification.
