#!/usr/bin/env python3
"""Guest augmentation is idempotent and fingerprints actual embedded inputs."""
import copy
import hashlib
import unittest
from sbom_guests import ROOT, augment, guest_components

class GuestTests(unittest.TestCase):
    def test_components_and_idempotency(self):
        data = {"metadata": {"component": {"bom-ref": "root"}}, "components": [],
                "dependencies": [{"ref": "root", "dependsOn": ["go-dependency"]}]}
        once = augment(data)
        self.assertEqual(augment(copy.deepcopy(once)), once)
        self.assertEqual(len(once["components"]), 2)
        for component in guest_components():
            path = ROOT / component["properties"][0]["value"]
            self.assertEqual(component["hashes"][0]["content"], hashlib.sha256(path.read_bytes()).hexdigest())
            self.assertIn(component["bom-ref"], once["dependencies"][0]["dependsOn"])
    def test_missing_root_rejects(self):
        with self.assertRaises(ValueError):
            augment({"metadata": {"component": {"bom-ref": "root"}}, "dependencies": []})

if __name__ == "__main__":
    unittest.main()
