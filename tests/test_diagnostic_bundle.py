# Fix for Issue #1: [$25 BOUNTY] [Python] Add diagnostic bundle validation tests

# tests/test_diagnostic_bundle.py
import os
import json
import unittest
from unittest.mock import patch
from build import generate_diagnostic_bundle

class TestDiagnosticBundle(unittest.TestCase):

    def test_diagnostic_bundle_generation(self):
        # Run the build script to generate the diagnostic bundle
        generate_diagnostic_bundle()

        # Get the list of files in the diagnostic directory
        diagnostic_dir = 'diagnostic'
        files = os.listdir(diagnostic_dir)

        # Find the generated JSON and logd files
        json_file = None
        logd_file = None
        for file in files:
            if file.startswith('build-') and file.endswith('.json'):
                json_file = os.path.join(diagnostic_dir, file)
            elif file.startswith('build-') and file.endswith('.logd'):
                logd_file = os.path.join(diagnostic_dir, file)

        # Check that both files were generated
        self.assertIsNotNone(json_file)
        self.assertIsNotNone(logd_file)

        # Check that the JSON file contains the expected information
        with open(json_file, 'r') as f:
            json_data = json.load(f)
            self.assertIn('metadata', json_data)
            self.assertIn('artifact_paths', json_data['metadata'])
            for path in json_data['metadata']['artifact_paths']:
                self.assertTrue(path.startswith('/'))

        # Check that local home/repo/temp paths and usernames are redacted
        self.assertNotIn('/home/', json_file)
        self.assertNotIn('/tmp/', json_file)
        self.assertNotIn('username', json_file)

        # Check that the logd file exists and is not empty
        self.assertGreater(os.path.getsize(logd_file), 0)

    def test_diagnostic_bundle_validation(self):
        # Test that the diagnostic bundle validation fails when the JSON file is missing
        with patch('os.listdir', return_value=['build-XXX.logd']):
            with self.assertRaises(FileNotFoundError):
                generate_diagnostic_bundle()

        # Test that the diagnostic bundle validation fails when the logd file is missing
        with patch('os.listdir', return_value=['build-XXX.json']):
            with self.assertRaises(FileNotFoundError):
                generate_diagnostic_bundle()

        # Test that the diagnostic bundle validation fails when the JSON and logd files do not match
        with patch('os.listdir', return_value=['build-XXX.json', 'build-YYY.logd']):
            with self.assertRaises(ValueError):
                generate_diagnostic_bundle()