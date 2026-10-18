# Mobile verification

Daily-brief checks from October 19, 2026:

- Go formatting, vet and the full race suite passed locally. PostgreSQL-backed tests require CI's disposable database; the new test checks inclusive/exclusive UTC-offset boundaries plus owner and personal/company isolation.
- Updated Android source compiled/dexed, the APK passed signature verification, and it installed over the existing app on the Moto G10 Power while preserving its app storage. Today was visually reviewed in the disconnected state, including compact navigation, the floating microphone dock and honest unloaded-history state.
- The production Android `DailyBrief` helper executed on-device with synthetic inputs: disjoint date windows, both MCP result envelopes, `has_more`, older-server refusal and nullable evidence projection passed.
- Shared SwiftUI source built with the real pinned Apple inference framework on Mac. Full unsigned iOS device compilation runs in CI.
- The dated brief requires the new server fields; production end-to-end retrieval/reflection has not been verified before that server update is deployed. This check does not establish the small model's summarization accuracy. No additional voice-quality or physical iPhone checks were performed.

Checks from the October 18, 2026 development session:

- Kotlin and Swift mcp-mobile 0.1.0 clients initialized, discovered the authenticated Cully catalog and performed synthetic, bounded read-only `cully_context` searches. No write tools were invoked. SDK CI passed Kotlin tests, Swift XCTest and iOS library compilation before the tag was released.
- Cully Android UI and SDK compiled and dexed for API 28; APK alignment/signature verification passed. On a Moto G10 Power / Android 11, the app connected and found eleven personal tools. Private test credential handoff was deleted after loading.
- The Android Qwen 0.5B model loaded the canonical Cully mobile skill, selected one `cully_context` call for a synthetic empty search, received the MCP result and answered locally. Measured turn including UI polling: 43.5 seconds. This is a functional smoke check, not an accuracy or latency benchmark.
- The pinned llama.cpp Apple framework checksum matched the release digest. Its CPU engine generated the expected synthetic response using the same existing GGUF on Mac. Shared SwiftUI source compiled on Mac.
- Go vet and all Go race tests passed. CI also checks skill freshness and matching pinned model manifests.

Not yet verified on a physical iPhone: installation/signing, OAuth/Keychain, on-device speech and full model-to-MCP chat. On Android the browser authorization/callback still needs an interactive account sign-in check; SDK connectivity was verified using a private read-only test session. Existing model files were used for inference checks; an uninterrupted full first-run model download on each mobile platform has not yet been measured. Android microphone interaction and audible output need a person to assess quality. These are preview limitations, not reported as passed.

No user conversations, private Cully note content, tokens or device identifiers are included in this repository or release evidence.
