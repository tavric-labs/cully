# Cully mobile clients

Cully Self is a personal companion: a calm Today brief, a private task list and focus, an Advisor, and an Ask Cully composer with a microphone. Android and iOS source live here; the reusable connection SDK lives in [mcp-mobile](https://github.com/tavric-labs/mcp-mobile), released as native Kotlin and Swift packages.

This is a developer preview. Workspace actions, coding terminals, phone dialer/camera/settings controls, calendar/contact access, and background agent execution are not exposed. Saved work notes are dated evidence, not live coding-session health. Task changes require explicit UI actions and stay on this device.

## Local intelligence

Tap **Set up Cully** to download pinned model weights. Engines and connection libraries ship with the app. Android downloads Qwen 0.5B Q4_K_M plus Whisper tiny Q5_1 (499 MiB total); iOS downloads Qwen (469 MiB) and uses Apple's on-device speech recognition when supported. Downloads show progress and verify size and SHA-256 before installation. Android retains partial downloads for resume; iOS currently restarts an interrupted download. Nothing downloads automatically. Allow 650 MiB of free space in addition to the app.

Inference runs locally, with no paid inference API. Reading Cully history still requires an internet connection and your Cully account/server. The preserved MiniLM embedding experiment is optional future retrieval work; neither app currently uses embeddings. An embedding model cannot replace speech recognition or tool selection.

The language model receives the same canonical `skills/cully/SKILL.md` identity through its short, hash-checked `references/mobile-self.md` profile. The host supplies a projection of the discovered `cully_context` schema limited to private, read-only lookups of at most three notes. The model chooses calls and arguments; the host validates, executes through mcp-mobile, and returns bounded evidence for the answer. No keyword-to-tool router is used. Two calls maximum per question; no automatic write tools. This small model can make mistakes and is slow on entry-level phones.

Regenerate bundled skill assets after changing the canonical skill:

```sh
python3 -m pip install -r apps/requirements-dev.txt
python3 apps/scripts/build_skill_assets.py
python3 apps/scripts/build_skill_assets.py --check
python3 apps/scripts/check_models.py
```

## Build and verification

- [Android](android/README.md): native Java UI, Kotlin SDK, bundled llama.cpp and Whisper executables; arm64 Android 9+ preview.
- [iOS](ios/README.md): SwiftUI, Swift SDK and pinned llama.cpp Apple framework; iOS 16.4+.
- [Verification](VERIFICATION.md) separates checks completed from device work still pending.
- [Third-party sources](THIRD_PARTY.md) documents pinned runtimes/models. Weights, private keys, OAuth sessions and device output do not belong in Git.
