# Cully for iOS

SwiftUI Self-mode developer preview for iOS 16+. It uses mcp-mobile 0.1.0, the official Swift MCP SDK, a pinned llama.cpp XCFramework and a user-downloaded Qwen model. Speech transcription is explicitly on-device; languages/devices without support can use typed questions.

## Build

Open `Cully.xcodeproj` in a compatible Xcode. The project is generated from `project.yml`:

```sh
brew install xcodegen
xcodegen generate --spec apps/ios/project.yml
xcodebuild -project apps/ios/Cully.xcodeproj -scheme Cully -configuration Debug -destination 'generic/platform=iOS' CODE_SIGNING_ALLOWED=NO build
```

Select your development team in Xcode to install on an iPhone. Device trust, Developer Mode and a compatible Xcode/iOS pairing are required. No signing identity or team is stored in source. App Store and TestFlight distribution are not configured.

A macOS SwiftUI build can check shared source with `swift build --package-path apps/ios`. `MCP_MOBILE_PATH` optionally points at a local SDK checkout. `CULLY_SKIP_LLAMA=1` is a layout-only compilation option that disables inference; the normal build includes the checksum-pinned Apple runtime. For an already verified local XCFramework, `CULLY_LLAMA_PATH` is a path relative to `apps/ios/Package.swift`.

## Preview behavior

Today, Tasks and Advisor share the Ask Cully composer and microphone. Tasks are local. Setup downloads 469 MiB of pinned weights after confirmation, verifies SHA-256 and excludes model storage from iCloud backup. The current iOS downloader restarts interrupted downloads. Inference is bounded CPU work, canceled on backgrounding; loading a model for each completion favors cleanup over latency in this first preview.

Browser sign-in uses the official SDK's OAuth authorizer, ASWebAuthenticationSession and device-only Keychain token storage. MCP connections close when the app pauses. Reconnect using the connection sheet; automatic background work is not enabled. The model can request the discovered, private `cully_context` tool; the host enforces read-only Self policy and at most three notes. No write tools or Workspace actions are exposed.

Source/build checks and the Apple inference runtime have been tested on Mac. Physical iPhone sign-in, microphone, Keychain and chat round trips remain device verification work. Device compilation without signing is enforced by Mobile clients CI.

The pinned upstream XCFramework contains iOS arm64 device and macOS slices, with no iOS Simulator slice. CI builds the complete app against the device SDK with signing disabled; use a physical iPhone for installation. A simulator-specific llama.cpp build is future work. This does not affect the standalone mcp-mobile SDK, whose simulator compilation is tested independently.
