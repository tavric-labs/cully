#!/usr/bin/env python3
"""Bundle the canonical Cully Agent Skill and its mobile profile for both native clients."""
import argparse
import hashlib
import json
from pathlib import Path
import yaml

ROOT = Path(__file__).resolve().parents[2]
SKILL = ROOT / 'skills/cully'
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
source = (SKILL / 'SKILL.md').read_text()
_, frontmatter, body = source.split('---', 2)
metadata = yaml.safe_load(frontmatter)
assert metadata['name'] == 'cully' and metadata['description']
entry = metadata['metadata']['mobile-profile']
assert entry == 'references/mobile-self.md'
files = {'SKILL.md': source, entry: (SKILL / entry).read_text(),
         'references/mobile-workflows.md': (SKILL / 'references/mobile-workflows.md').read_text()}
catalog = {'format': 'agent-skills', 'name': metadata['name'], 'description': metadata['description'],
           'profile': entry, 'source': 'skills/cully/SKILL.md',
           'sha256': {name: hashlib.sha256(value.encode()).hexdigest() for name,value in files.items()}}
files['catalog.json'] = json.dumps(catalog, indent=2) + '\n'
for target in [ROOT / 'apps/android/android/assets/skills/cully', ROOT / 'apps/ios/Sources/Skills/cully']:
    for name, value in files.items():
        output = target / name
        if args.check:
            if not output.is_file() or output.read_text() != value:
                raise SystemExit('Skill bundle is stale: ' + str(output.relative_to(ROOT)))
        else:
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(value)
print('Canonical Cully skill and mobile profile bundles are current.')
