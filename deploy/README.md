# Blue Polar Bear: Container Deployment

This directory contains container specifications for the multi-container tactical mesh stack.

### Service Containers

| Container | Purpose | Build Context |
| :--- | :--- | :--- |
| `Dockerfile.c2-gateway` | Tactical C2 Gateway WebSocket & REST Hub (Port `8080`) | Root |
| `Dockerfile.cds-guard` | Zero-Trust Cross Domain Solution Guard (Port `8081`) | Root |
| `Dockerfile.edge-agent` | 10-Drone Swarm Telemetry Simulator | Root |

### Quick Commands

```bash
# Build and start all services via Docker Compose
docker compose up -d --build

# View container status
docker compose ps

# Tail live logs
docker compose logs -f

# Tear down the stack
docker compose down
```

For Makefile shortcuts and native execution, see [Development Guide](../docs/development.md).
