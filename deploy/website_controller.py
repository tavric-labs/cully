#!/usr/bin/env python3
"""Restricted SSH controller for the independent Cully website and docs projects."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path('/opt/cully-web')
REVISION = re.compile(r'[0-9a-f]{40}')
SITES = {
    'website': {
        'state': 'state.json',  # Preserve the existing website deployment history.
        'compose': 'compose.yaml',
        'project': 'cully-web',
        'revision_var': 'CULLY_WEB_REVISION',
        'image': 'ghcr.io/tavric-labs/cully-web:',
    },
    'docs': {
        'state': 'docs-state.json',
        'compose': 'compose.docs.yaml',
        'project': 'cully-docs',
        'revision_var': 'CULLY_DOCS_REVISION',
        'image': 'ghcr.io/tavric-labs/cully-docs:',
    },
}


def validate_request(request):
    if not isinstance(request, dict) or request.get('action') not in (
            'deploy-website', 'rollback-website', 'deploy-docs', 'rollback-docs'):
        raise ValueError('unsupported deployment action')
    if not isinstance(request.get('revision'), str) or not REVISION.fullmatch(request['revision']):
        raise ValueError('invalid deployment revision')
    if request['action'].startswith('deploy-'):
        token = request.get('token')
        user = request.get('registry_user')
        if not isinstance(token, str) or not 1 <= len(token) <= 8192 or '\n' in token:
            raise ValueError('missing registry credential')
        if not isinstance(user, str) or not re.fullmatch(r'[A-Za-z0-9_\[\]-]{1,100}', user):
            raise ValueError('invalid registry user')


def write_state(state_file, state):
    with tempfile.NamedTemporaryFile(mode='w', dir=state_file.parent, delete=False) as f:
        json.dump(state, f)
        temporary = Path(f.name)
    os.replace(temporary, state_file)


def compose(root, site, revision, runner, docker_config=None):
    config = SITES[site]
    env = dict(os.environ, **{config['revision_var']: revision})
    if docker_config is not None:
        env['DOCKER_CONFIG'] = docker_config
    runner(['docker', 'compose', '--project-name', config['project'], '--file',
            str(root / config['compose']), 'up', '-d', '--wait', '--wait-timeout', '120'],
           env=env, check=True)


def stop(root, site, revision, runner):
    config = SITES[site]
    env = dict(os.environ, **{config['revision_var']: revision})
    runner(['docker', 'compose', '--project-name', config['project'], '--file',
            str(root / config['compose']), 'down'], env=env, check=True)


def execute(request, root=ROOT, runner=subprocess.run):
    validate_request(request)
    action, site = request['action'].split('-', 1)
    config = SITES[site]
    state_file = root / config['state']
    state = json.loads(state_file.read_text()) if state_file.exists() else {}
    for revision in state.values():
        if revision is not None and (not isinstance(revision, str) or not REVISION.fullmatch(revision)):
            raise ValueError('invalid saved deployment state')
    current = state.get('current')
    if action == 'rollback':
        if current != request['revision']:
            raise ValueError('refusing to roll back a different deployment')
        previous = state.get('previous')
        if not previous:
            stop(root, site, current, runner)
            write_state(state_file, {'current': None, 'previous': current})
            return None
        compose(root, site, previous, runner)
        write_state(state_file, {'current': previous, 'previous': current})
        return previous

    revision = request['revision']
    with tempfile.TemporaryDirectory(prefix='cully-registry-') as directory:
        # The job-scoped registry token is supplied via stdin and removed with
        # this temporary Docker config. It never appears in command arguments.
        runner(['docker', '--config', directory, 'login', 'ghcr.io', '--username',
                request['registry_user'], '--password-stdin'],
               input=request['token'], text=True, check=True, capture_output=True)
        image = config['image'] + revision
        runner(['docker', '--config', directory, 'pull', image], check=True)
        try:
            compose(root, site, revision, runner, directory)
        except subprocess.CalledProcessError:
            if current:
                compose(root, site, current, runner)
            else:
                stop(root, site, revision, runner)
            raise
    previous = state.get('previous') if revision == current else current
    write_state(state_file, {'current': revision, 'previous': previous})
    return revision


def main():
    if os.environ.get('SSH_ORIGINAL_COMMAND') != 'deploy':
        raise ValueError('only the deploy SSH command is allowed')
    raw = sys.stdin.buffer.read(16385)
    if len(raw) > 16384:
        raise ValueError('deployment request too large')
    revision = execute(json.loads(raw))
    print('Deployment revision active:', revision)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        # Never print the input request, token, or subprocess captured output.
        print('Website deployment failed:', type(error).__name__, file=sys.stderr)
        sys.exit(1)
