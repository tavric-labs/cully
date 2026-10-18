# Cully for Android

Developer preview for arm64 Android 9+ (API 28). The original development application ID `dev.local.aiphone` is retained so updates preserve existing private tasks and downloaded models. This debug-signed preview targets API 30 and is not a Play Store production build.

The three navigation destinations are Today, Tasks and Advisor. Ask Cully and its microphone stay available below the active page. Voice starts only on an explicit tap. Whisper transcribes locally; installed offline Android TTS reads replies when available. Models and tasks remain in app storage. Backgrounding stops inference and recording and closes the MCP session. Returning reconnects the retained in-memory credential; restarting the process may require browser sign-in again.

## Build

Install JDK 17, Android SDK platform 30, build-tools 35.0.0, CMake, Ninja and an Android NDK. Clone mcp-mobile at `v0.1.0`, then build the pinned official Kotlin SDK dependencies:

```sh
mvn -B -ntp -f ../mcp-mobile/pom.xml verify dependency:copy-dependencies -DincludeScope=runtime
python3 apps/android/scripts/build_app.py --compile-only --engine-home /path/to/engine-workspace --mcp-sdk ../mcp-mobile/target --android-sdk "$ANDROID_HOME"
```

To package inference, keep native dependencies outside the repository. Extract the checksum-verified `llama-b11718-android-arm64.tar.gz` release into `ENGINE_HOME/vendor/llama-android`. Clone whisper.cpp `v1.9.5` into `ENGINE_HOME/vendor/whisper.cpp-1.9.5` and verify commit `d1be6fde11ac6e0407606b4e42fe72d34add8037`. Build it with `build_voice.py` below. The NDK must be installed under `ENGINE_HOME/toolchains/android-sdk/ndk` or supplied through `--android-sdk`.

```sh
python3 apps/android/scripts/build_voice.py --engine-home /path/to/engine-workspace --android-sdk "$ANDROID_HOME"
python3 apps/android/scripts/build_app.py --engine-home /path/to/engine-workspace --mcp-sdk ../mcp-mobile/target --android-sdk "$ANDROID_HOME"
adb install -r apps/android/build/cully-debug.apk
```

The builder creates a development keystore inside `ENGINE_HOME/private`, outside Git. Keep it to update the same installed app. Models are intentionally absent from the APK: tap Set up Cully after installing. The signing key and model/runtime workspace must never be committed.

## Cully sign-in

Connect Cully opens an HTTPS MCP endpoint's OAuth authorization URL in Android's default browser using an ACTION_VIEW browsable intent. The app uses a public client with PKCE, validates state, and exchanges the callback in `dev.cully.mobile:/oauth2redirect`. Request scope is `tools:read`. HTTPS, working DNS, a correct phone clock and an authorized account are required. Tokens remain in memory; service secrets are never entered in the app. The development-only private test-session handoff deletes itself after reading and is not a public login API.
