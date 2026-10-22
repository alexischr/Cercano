#!/usr/bin/env python3
"""Read-only provenance gate for tap-only recovery from an existing CI run."""
import argparse
import os
import re
from update_tap import api, Failure, require
from render_formula import parse_version, ValidationError

BASE = '/repos/cercano-ai/Cercano/'


def check(version, run_id):
    parse_version(version)
    require(bool(re.fullmatch(r'[1-9][0-9]*', run_id)), 'Invalid Actions run ID')
    token = os.environ.get('GITHUB_TOKEN', '')
    release = api('GET', BASE + 'releases/tags/v' + version, token)
    require(release.get('tag_name') == 'v' + version and release.get('draft') is False,
            'Requested release is not published')
    commit = api('GET', BASE + 'commits/v' + version, token)
    run = api('GET', BASE + 'actions/runs/' + run_id, token)
    require(run.get('head_sha') == commit.get('sha') and bool(commit.get('sha')),
            'CI run does not match the published tag commit')
    require(run.get('path') == '.github/workflows/release-macos.yml'
            and run.get('event') == 'workflow_dispatch'
            and run.get('status') == 'completed'
            and run.get('head_repository', {}).get('full_name') == 'cercano-ai/Cercano',
            'Not a completed trusted release workflow run')
    data = api('GET', BASE + 'actions/runs/' + run_id + '/jobs?filter=latest&per_page=100', token)
    require(data.get('total_count', 0) <= 100, 'Unexpected job count')
    jobs = data.get('jobs', [])
    consolidated = [j for j in jobs if j.get('name') ==
                    'Build, sign, notarize, verify and release']
    if consolidated:
        require(len(consolidated) == 1, 'Ambiguous release job')
        # A tap failure must not force a rebuild or replacement of released bytes.
        # Require the actual verification, artifact retention and publication
        # steps to have succeeded, not the overall job (which includes the tap).
        for name in ('Verify finished archive', 'Upload artifacts for review',
                     'Refuse to replace an existing published artifact', 'Publish release'):
            matches = [s for s in consolidated[0].get('steps', []) if s.get('name') == name]
            require(len(matches) == 1 and matches[0].get('conclusion') == 'success',
                    'Successful verification and publication steps are required')
        required_jobs = ('Build unsigned experimental Windows x64 archive',
                         'Build unsigned experimental Linux x86_64 archive')
    else:
        # Backward-compatible recovery for releases made before consolidation.
        required_jobs = ('Build, sign, notarize and verify', 'Publish to GitHub Releases')
    for name in required_jobs:
        matches = [j for j in jobs if j.get('name') == name]
        require(len(matches) == 1 and matches[0].get('conclusion') == 'success',
                'Successful release prerequisite jobs are required')
    return commit['sha']


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--version', required=True)
    p.add_argument('--run-id', required=True)
    a = p.parse_args()
    try:
        print('Verified release source:', check(a.version, a.run_id))
    except (Failure, ValidationError, ValueError, KeyError):
        raise SystemExit('Release provenance validation failed; no tap update attempted')
