# Fix for Issue #1: [$25 BOUNTY] [Python] Add diagnostic bundle validation tests

# build.py
import os
import json
import subprocess

def generate_diagnostic_bundle():
    # Run the build command to generate the diagnostic bundle
    subprocess.run(['python3', 'build.py'])

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
    if json_file is None or logd_file is None:
        raise FileNotFoundError('Diagnostic bundle files not found')

    # Check that the JSON and logd files match
    if not json_file.split('.')[0] == logd_file.split('.')[0]:
        raise ValueError('Diagnostic bundle files do not match')

    # Check that the JSON file contains the expected information
    with open(json_file, 'r') as f:
        json_data = json.load(f)
        if 'metadata' not in json_data or 'artifact_paths' not in json_data['metadata']:
            raise ValueError('Invalid diagnostic metadata')

        for path in json_data['metadata']['artifact_paths']:
            if not path.startswith('/'):
                raise ValueError('Invalid artifact path')

    # Check that local home/repo/temp paths and usernames are redacted
    with open(json_file, 'r') as f:
        json_data = json.load(f)
        if '/home/' in str(json_data) or '/tmp/' in str(json_data) or 'username' in str(json_data):
            raise ValueError('Diagnostic metadata not redacted')