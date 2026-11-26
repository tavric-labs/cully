# Cully voice: speech to text with faster-whisper, on this machine's CPU.
# `stt.py --warmup` loads (and so downloads) the model once. Without it, the
# script serves requests: one audio file path per line on stdin, one JSON line
# per answer on stdout. Audio never leaves the machine.
import json
import os
import sys

from faster_whisper import WhisperModel

name = os.environ.get("CULLY_STT_MODEL", "distil-large-v3")
threads = int(os.environ.get("CULLY_STT_THREADS", "8"))
model = WhisperModel(name, device="cpu", compute_type="int8", cpu_threads=threads)

if "--warmup" in sys.argv:
    print("ready", flush=True)
    sys.exit(0)

for line in sys.stdin:
    path = line.strip()
    if not path:
        continue
    try:
        segments, _ = model.transcribe(path, beam_size=5, vad_filter=True, condition_on_previous_text=False)
        print(json.dumps({"text": " ".join(s.text.strip() for s in segments).strip()}), flush=True)
    except Exception as e:  # keep serving after a bad file
        print(json.dumps({"error": str(e)[:200]}), flush=True)
