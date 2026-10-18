# Mobile verification

Checks from the October 18, 2026 development session:

- Kotlin and Swift mcp-mobile 0.1.0 clients initialized, discovered the authenticated Cully catalog and performed synthetic, bounded read-only `cully_context` searches. No write tools were invoked. SDK CI passed Kotlin tests, Swift XCTest and iOS library compilation before the tag was released.
- Cully Android UI and SDK compiled and dexed for API 28; APK alignment/signature verification passed. On a Moto G10 Power / Android 11, the app connected and found eleven personal tools. Private test credential handoff was deleted after loading.
- The Android Qwen 0.5B model loaded the canonical Cully mobile skill, selected one `cully_context` call for a synthetic empty search, received the MCP result and answered locally. Measured turn including UI polling: 43.5 seconds. This is a functional smoke check, not an accuracy or latency benchmark.
- The pinned llama.cpp Apple framework checksum matched the release digest. Its CPU engine generated the expected synthetic response using the same existing GGUF on Mac. Shared SwiftUI source compiled on Mac.
- Go vet and all Go race tests passed. CI also checks skill freshness and matching pinned model manifests.

Not yet verified on a physical iPhone: installation/signing, OAuth/Keychain, on-device speech and full model-to-MCP chat. On Android the browser authorization/callback still needs an interactive account sign-in check; SDK connectivity was verified using a private read-only test session. Existing model files were used for inference checks; an uninterrupted full first-run model download on each mobile platform has not yet been measured. Android microphone interaction and audible output need a person to assess quality. These are preview limitations, not reported as passed.

No user conversations, private Cully note content, tokens or device identifiers are included in this repository or release evidence.
