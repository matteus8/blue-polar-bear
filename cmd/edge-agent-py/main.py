#!/usr/bin/env python3
"""
Blue Polar Bear: Python Edge Companion Agent (Raspberry Pi 5)
Simulates physical vehicle telemetry and handles tactical C2 instructions via Eclipse Zenoh.
"""

import argparse
import dataclasses
import hashlib
import json
import logging
import math
import sys
import time
from typing import Any, Dict, List, Optional

try:
    import zenoh
except ImportError:
    zenoh = None

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] [%(name)s] %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("edge-agent-py")

# Synthetic Classification Constants (Never use real USG designations)
TIER_1_PUBLIC = "TIER-1: PUBLIC"
TIER_2_RESTRICTED = "TIER-2: RESTRICTED"
TIER_3_CRITICAL = "TIER-3: CRITICAL"

TIER_KEY_MAP = {
    TIER_1_PUBLIC: "tier1",
    TIER_2_RESTRICTED: "tier2",
    TIER_3_CRITICAL: "tier3",
}


@dataclasses.dataclass
class Coordinates:
    lat: float
    lon: float
    alt_m: float

    def to_dict(self) -> Dict[str, float]:
        return {
            "lat": round(self.lat, 6),
            "lon": round(self.lon, 6),
            "alt_m": round(self.alt_m, 2),
        }


@dataclasses.dataclass
class Velocity:
    vx: float
    vy: float
    vz: float
    speed_mps: float

    def to_dict(self) -> Dict[str, float]:
        return {
            "vx": round(self.vx, 2),
            "vy": round(self.vy, 2),
            "vz": round(self.vz, 2),
            "speed_mps": round(self.speed_mps, 2),
        }


class VehicleSimulator:
    """Simulates realistic flight telemetry, battery decay, and state transitions."""

    def __init__(
        self,
        vehicle_id: str,
        vehicle_type: str,
        team: str,
        center_lat: float = 37.7749,
        center_lon: float = -122.4194,
    ) -> None:
        self.vehicle_id = vehicle_id
        self.vehicle_type = vehicle_type
        self.team = team
        self.state = "AIRBORNE"
        self.battery_pct = 99.0
        self.center_lat = center_lat
        self.center_lon = center_lon
        self.current_alt = 115.0
        self.speed_mps = 16.0
        self.flight_angle = 0.0
        self.orbit_radius = 0.0075
        self.sequence = 0
        self.ew_active = False

    def step(self, dt: float) -> Dict[str, Any]:
        self.sequence += 1
        self.flight_angle += 0.07 * dt
        if self.flight_angle > 2 * math.pi:
            self.flight_angle -= 2 * math.pi

        if self.state in ("AIRBORNE", "PATROL"):
            self.battery_pct = max(5.0, self.battery_pct - (0.04 * dt))
            lat = self.center_lat + (self.orbit_radius * math.sin(self.flight_angle))
            lon = self.center_lon + (self.orbit_radius * math.cos(self.flight_angle))
            heading = ((self.flight_angle * 180 / math.pi) + 90) % 360
            alt = self.current_alt + (4.0 * math.sin(self.flight_angle * 3))

            coords = Coordinates(lat=lat, lon=lon, alt_m=alt)
            vel = Velocity(
                vx=self.speed_mps * math.cos(math.radians(heading)),
                vy=self.speed_mps * math.sin(math.radians(heading)),
                vz=0.0,
                speed_mps=self.speed_mps,
            )
        elif self.state == "HOVER":
            self.battery_pct = max(5.0, self.battery_pct - (0.02 * dt))
            coords = Coordinates(lat=self.center_lat, lon=self.center_lon, alt_m=self.current_alt)
            vel = Velocity(vx=0.0, vy=0.0, vz=0.0, speed_mps=0.0)
            heading = 0.0
        elif self.state == "RTB":
            self.current_alt = max(0.0, self.current_alt - (12.0 * dt))
            self.battery_pct = max(5.0, self.battery_pct - (0.03 * dt))
            if self.current_alt <= 2.0:
                self.state = "LANDED"
            coords = Coordinates(lat=self.center_lat, lon=self.center_lon, alt_m=self.current_alt)
            vel = Velocity(vx=0.0, vy=0.0, vz=-2.0, speed_mps=6.0)
            heading = 0.0
        else:
            coords = Coordinates(lat=self.center_lat, lon=self.center_lon, alt_m=0.0)
            vel = Velocity(vx=0.0, vy=0.0, vz=0.0, speed_mps=0.0)
            heading = 0.0

        return {
            "vehicle_id": self.vehicle_id,
            "vehicle_type": self.vehicle_type,
            "team": self.team,
            "state": self.state,
            "battery_pct": round(self.battery_pct, 1),
            "coordinates": coords.to_dict(),
            "velocity": vel.to_dict(),
            "heading_deg": round(heading, 1),
            "mission_payload": {
                "optical_tracker": "LOCKED",
                "ew_active": self.ew_active,
                "companion_cpu_temp_c": 44.2,
            },
            "sequence": self.sequence,
        }

    def handle_command(self, cmd_type: str) -> None:
        logger.info("Executing command instruction: %s", cmd_type)
        if cmd_type == "RETURN_TO_BASE":
            self.state = "RTB"
        elif cmd_type == "HOVER":
            self.state = "HOVER"
        elif cmd_type == "PATROL":
            self.state = "PATROL"
        elif cmd_type == "ARM":
            self.state = "AIRBORNE"
            self.current_alt = 100.0
        elif cmd_type == "DISARM":
            self.state = "LANDED"
            self.current_alt = 0.0


def compute_digest(payload: Dict[str, Any]) -> str:
    """Computes SHA-256 hash matching Go schema implementation."""
    raw = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return hashlib.sha256(raw).hexdigest()


def create_security_envelope(
    classification: str,
    origin_enclave: str,
    telemetry_payload: Dict[str, Any],
    caveats: Optional[List[str]] = None,
) -> Dict[str, Any]:
    digest = compute_digest(telemetry_payload)
    timestamp_ns = time.time_ns()
    msg_id = f"msg-py-{timestamp_ns}-{telemetry_payload['vehicle_id']}"

    return {
        "header": {
            "classification": classification,
            "origin_enclave": origin_enclave,
            "timestamp_ns": timestamp_ns,
            "message_id": msg_id,
            "digest": digest,
            "caveats": caveats or [],
        },
        "telemetry": telemetry_payload,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Blue Polar Bear Python Edge Agent (Pi 5)")
    parser.add_argument("--id", default="alpha", help="Vehicle unit ID (e.g. alpha, bravo)")
    parser.add_argument("--type", default="drone", help="Vehicle type (e.g. drone, rover)")
    parser.add_argument("--team", default="blue", help="Team identifier")
    parser.add_argument("--rate", type=float, default=1.0, help="Publish rate in Hz")
    parser.add_argument("--config", default="configs/zenoh-edge.json5", help="Zenoh config path")
    parser.add_argument(
        "--tier",
        default=TIER_2_RESTRICTED,
        choices=[TIER_1_PUBLIC, TIER_2_RESTRICTED, TIER_3_CRITICAL],
        help="Classification tier to publish under",
    )
    args = parser.parse_args()

    logger.info("Initializing Edge Agent [%s/%s/%s]", args.type, args.team, args.id)

    if zenoh is None:
        logger.error("Zenoh Python library not found. Run in an environment with eclipse-zenoh installed.")
        sys.exit(1)

    # Open Zenoh session
    try:
        zenoh_conf = zenoh.Config.from_file(args.config) if args.config else zenoh.Config()
    except Exception as err:
        logger.warning("Could not read config %s: %s. Using default Zenoh config.", args.config, err)
        zenoh_conf = zenoh.Config()

    session = zenoh.open(zenoh_conf)
    tier_token = TIER_KEY_MAP[args.tier]
    telem_key = f"sec/{tier_token}/{args.type}/{args.team}/{args.id}/telemetry"
    cmd_key = f"sec/tier2/{args.type}/{args.team}/{args.id}/command"

    sim = VehicleSimulator(args.id, args.type, args.team)

    # Command listener
    def on_command(sample: Any) -> None:
        try:
            raw_bytes = bytes(sample.payload)
            data = json.loads(raw_bytes.decode("utf-8"))
            cmd = data.get("command", {})
            cmd_type = cmd.get("command_type", "")
            if cmd_type:
                sim.handle_command(cmd_type)
        except Exception as e:
            logger.error("Failed to parse command sample: %s", e)

    sub = session.declare_subscriber(cmd_key, on_command)
    pub = session.declare_publisher(telem_key)

    logger.info("Subscribed to C2 commands on: %s", cmd_key)
    logger.info("Publishing telemetry to: %s at %.1f Hz", telem_key, args.rate)

    interval = 1.0 / args.rate
    try:
        while True:
            t_start = time.time()
            telem = sim.step(interval)
            env = create_security_envelope(
                classification=args.tier,
                origin_enclave="edge-node-01-pi5-py",
                telemetry_payload=telem,
            )
            payload_str = json.dumps(env)
            pub.put(payload_str)

            elapsed = time.time() - t_start
            sleep_time = max(0.01, interval - elapsed)
            time.sleep(sleep_time)
    except KeyboardInterrupt:
        logger.info("Agent interrupted by operator. Shutting down...")
    finally:
        session.close()
        logger.info("Zenoh session closed.")


if __name__ == "__main__":
    main()
