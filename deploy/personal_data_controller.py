#!/usr/bin/env python3
"""Restricted SSH controller for release-tagged personal Cully services."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path('/opt/cully-personal-data')
TAG = re.compile(r'v[0-9]+\.[0-9]+\.[0-9]+(?:[-.][A-Za-z0-9.-]+)?')
USER = re.compile(r'[A-Za-z0-9_\[\]-]{1,100}')


def validate_request(request):
    if not isinstance(request, dict) or request.get('action') not in ('deploy', 'rollback'):
        raise ValueError('unsupported deployment action')
    if not isinstance(request.get('tag'), str) or not TAG.fullmatch(request['tag']):
        raise ValueError('invalid release tag')
    if request['action'] == 'deploy':
        token = request.get('token')
        user = request.get('registry_user')
        if not isinstance(token, str) or not 1 <= len(token) <= 8192 or '\n' in token:
            raise ValueError('missing registry credential')
        if not isinstance(user, str) or not USER.fullmatch(user):
            raise ValueError('invalid registry user')


def write_state(root, state):
    with tempfile.NamedTemporaryFile(mode='w', dir=root, delete=False) as f:
        json.dump(state, f)
        temporary = Path(f.name)
    os.replace(temporary, root / 'state.json')


def compose(root, tag, runner, docker_config=None):
    env = dict(os.environ, CULLY_DATA_TAG=tag)
    if docker_config is not None:
        env['DOCKER_CONFIG'] = docker_config
    base = ['docker', 'compose', '--project-name', 'cully-personal-data',
            '--project-directory', str(root), '--file', str(root / 'compose.yaml')]
    runner(base + ['up', '-d', '--wait', '--wait-timeout', '120', 'db', 'mem0-db'],
           cwd=root, env=env, check=True)
    runner(base + ['--profile', 'ops', 'run', '--rm', 'migrate'],
           cwd=root, env=env, check=True)
    runner(base + ['up', '-d', '--wait', '--wait-timeout', '180', 'mem0', 'cully-data-api'],
           cwd=root, env=env, check=True)


def stop(root, tag, runner):
    env = dict(os.environ, CULLY_DATA_TAG=tag)
    runner(['docker', 'compose', '--project-name', 'cully-personal-data',
            '--project-directory', str(root), '--file', str(root / 'compose.yaml'),
            'down'], cwd=root, env=env, check=True)


def execute(request, root=ROOT, runner=subprocess.run):
    validate_request(request)
    if not (root / '.env').is_file():
        raise ValueError('personal data configuration is missing')
    state_file = root / 'state.json'
    state = json.loads(state_file.read_text()) if state_file.exists() else {}
    for tag in state.values():
        if tag is not None and (not isinstance(tag, str) or not TAG.fullmatch(tag)):
            raise ValueError('invalid saved deployment state')
    current = state.get('current')
    if request['action'] == 'rollback':
        if current != request['tag']:
            raise ValueError('refusing to roll back a different deployment')
        previous = state.get('previous')
        if not previous:
            stop(root, current, runner)
            write_state(root, {'current': None, 'previous': current})
            return None
        compose(root, previous, runner)
        write_state(root, {'current': previous, 'previous': current})
        return previous

    tag = request['tag']
    with tempfile.TemporaryDirectory(prefix='cully-registry-') as directory:
        runner(['docker', '--config', directory, 'login', 'ghcr.io', '--username',
                request['registry_user'], '--password-stdin'],
               input=request['token'], text=True, check=True, capture_output=True)
        runner(['docker', '--config', directory, 'pull', 'ghcr.io/tavric-labs/cully-data:' + tag], check=True)
        runner(['docker', '--config', directory, 'pull', 'ghcr.io/tavric-labs/cully-mem0:' + tag], check=True)
        try:
            compose(root, tag, runner, directory)
        except subprocess.CalledProcessError:
            if current:
                compose(root, current, runner)
            else:
                stop(root, tag, runner)
            raise
    previous = state.get('previous') if tag == current else current
    write_state(root, {'current': tag, 'previous': previous})
    return tag


def main():
    if os.environ.get('SSH_ORIGINAL_COMMAND') != 'deploy':
        raise ValueError('only the deploy SSH command is allowed')
    raw = sys.stdin.buffer.read(16385)
    if len(raw) > 16384:
        raise ValueError('deployment request too large')
    tag = execute(json.loads(raw))
    print('Personal data release active:', tag)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print('Personal data deployment failed:', type(error).__name__, file=sys.stderr)
        sys.exit(1)
