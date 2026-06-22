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

        # Find the generated diagnostic bundle files
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

        # Check that the JSON file contains the expected metadata
        with open(json_file, 'r') as f:
            metadata = json.load(f)
            self.assertTrue('repository_relative_paths' in metadata)
            self.assertTrue('redacted_metadata' in metadata)

        # Check that local home/repo/temp paths and usernames are redacted
        self.assertNotIn('/home/', json.dumps(metadata))
        self.assertNotIn('/repo/', json.dumps(metadata))
        self.assertNotIn('/temp/', json.dumps(metadata))
        self.assertNotIn('username', json.dumps(metadata))

        # Check that the .logd file exists and is not empty
        self.assertGreater(os.path.getsize(logd_file), 0)

    def test_diagnostic_bundle_validation(self):
        # Test that the diagnostic bundle validation fails when the JSON file is missing
        with patch('os.listdir', return_value=['build-XXX.logd']):
            with self.assertRaises(FileNotFoundError):
                generate_diagnostic_bundle()

        # Test that the diagnostic bundle validation fails when the .logd file is missing
        with patch('os.listdir', return_value=['build-XXX.json']):
            with self.assertRaises(FileNotFoundError):
                generate_diagnostic_bundle()

        # Test that the diagnostic bundle validation fails when the pair is mismatched
        with patch('os.listdir', return_value=['build-XXX.json', 'build-YYY.logd']):
            with self.assertRaises(ValueError):
                generate_diagnostic_bundle()

# build.py
import os
import json
import subprocess

def generate_diagnostic_bundle():
    # Generate the diagnostic bundle
    subprocess.run(['python3', 'build.py'])

    # Get the list of files in the diagnostic directory
    diagnostic_dir = 'diagnostic'
    files = os.listdir(diagnostic_dir)

    # Find the generated diagnostic bundle files
    json_file = None
    logd_file = None
    for file in files:
        if file.startswith('build-') and file.endswith('.json'):
            json_file = os.path.join(diagnostic_dir, file)
        elif file.startswith('build-') and file.endswith('.logd'):
            logd_file = os.path.join(diagnostic_dir, file)

    # Check that both files were generated
    if json_file is None or logd_file is None:
        raise FileNotFoundError('Diagnostic bundle files not found')

    # Check that the pair is matched
    if os.path.basename(json_file).split('.')[0] != os.path.basename(logd_file).split('.')[0]:
        raise ValueError('Diagnostic bundle pair is mismatched')

    # Check that the JSON file contains the expected metadata
    with open(json_file, 'r') as f:
        metadata = json.load(f)
        if 'repository_relative_paths' not in metadata or 'redacted_metadata' not in metadata:
            raise ValueError('Diagnostic bundle metadata is invalid')

    # Check that local home/repo/temp paths and usernames are redacted
    with open(json_file, 'r') as f:
        metadata = json.load(f)
        if '/home/' in json.dumps(metadata) or '/repo/' in json.dumps(metadata) or '/temp/' in json.dumps(metadata) or 'username' in json.dumps(metadata):
            raise ValueError('Diagnostic bundle metadata is not redacted')