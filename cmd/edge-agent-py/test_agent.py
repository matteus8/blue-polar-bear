#!/usr/bin/env python3
"""
Unit tests for Python Edge Agent data models, physics step, and security envelopes.
"""

import unittest
from main import (
    VehicleSimulator,
    compute_digest,
    create_security_envelope,
    TIER_2_RESTRICTED,
)


class TestPythonEdgeAgent(unittest.TestCase):
    def test_simulator_step(self):
        sim = VehicleSimulator("bravo", "drone", "blue")
        t1 = sim.step(1.0)
        self.assertEqual(t1["vehicle_id"], "bravo")
        self.assertEqual(t1["state"], "AIRBORNE")
        self.assertTrue(98.0 <= t1["battery_pct"] <= 99.0)
        self.assertIn("lat", t1["coordinates"])
        self.assertIn("lon", t1["coordinates"])
        self.assertIn("alt_m", t1["coordinates"])

    def test_command_handling(self):
        sim = VehicleSimulator("bravo", "drone", "blue")
        sim.handle_command("RETURN_TO_BASE")
        self.assertEqual(sim.state, "RTB")
        sim.handle_command("HOVER")
        self.assertEqual(sim.state, "HOVER")
        sim.handle_command("PATROL")
        self.assertEqual(sim.state, "PATROL")

    def test_security_envelope_generation(self):
        sim = VehicleSimulator("bravo", "drone", "blue")
        telem = sim.step(1.0)
        env = create_security_envelope(
            classification=TIER_2_RESTRICTED,
            origin_enclave="test-enclave",
            telemetry_payload=telem,
        )

        self.assertEqual(env["header"]["classification"], TIER_2_RESTRICTED)
        self.assertEqual(env["header"]["origin_enclave"], "test-enclave")
        self.assertTrue(len(env["header"]["digest"]) == 64)
        self.assertEqual(env["header"]["digest"], compute_digest(telem))


if __name__ == "__main__":
    unittest.main()
