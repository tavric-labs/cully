#!/usr/bin/env python3
"""Keep native download manifests aligned with the reviewed, pinned source manifest."""
import json
from pathlib import Path
root = Path(__file__).resolve().parents[1]
source = (root / 'models.json').read_bytes()
manifest = json.loads(source)
for model in manifest['models']:
    assert model['url'].startswith('https://') and '/resolve/' in model['url']
    assert len(model['sha256']) == 64 and all(c in '0123456789abcdef' for c in model['sha256'])
    assert model['bytes'] > 0 and Path(model['file']).name == model['file']
for path in ['android/android/assets/models.json', 'ios/Sources/Models/models.json']:
    assert (root / path).read_bytes() == source, 'Model manifest is stale: ' + path
print('Pinned native model manifests match.')
