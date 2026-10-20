---
title: Cully Self on mobile
description: The Android and iOS developer preview for dated work briefs, private tasks and local-model questions over your own Cully memory.
---

# Cully Self on mobile

Cully Self is an Android and iOS **developer preview** for returning to your own work: a dated Today brief, local personal tasks, an Advisor and an Ask Cully composer with explicit microphone capture. It uses private personal Cully notes as evidence.

The preview is built from source. App Store, TestFlight and Play Store distribution are not configured. Start with the [mobile source and build guide](https://github.com/tavric-labs/cully/tree/main/apps).

## What you can do

| Surface | Current behavior |
| --- | --- |
| Today | Last 24 hours and a sampled seven-day view with dates, project links and coverage |
| Continue today | Up to three last-planned handoffs from the sampled week; historical suggestions, not live task status |
| Recall how / why | Saved approach, outcome, blocker and lesson when the note contains them |
| Learn next | Explicit recorded learnings and optional draft practice questions |
| Tasks | Create or change device-local tasks through an explicit confirmation |
| Ask Cully | Typed or on-device voice questions, bounded read-only recall and a local-model answer |

The brief retrieves at most three previews from each of two windows: the last 24 hours and the preceding six days. It shows when more notes exist. Empty or partial history is not a complete activity report. Older servers that lack date-window support show an update-needed state.

When models are ready, the app can draft a local reflection over the loaded evidence. Drafts remain suggestions for review; the app does not infer your expertise from a technology mentioned in a note. Reading recommendations and scheduled recaps remain planned.

## Setup and data boundaries

Connect to your authorized HTTPS Cully MCP endpoint through browser sign-in. Reading history needs network access. Task storage and inference are local; tasks do not synchronize into Team workspace records. Notes and reflections are cleared from memory on pause or disconnect.

Model setup is an explicit download with pinned size and SHA-256 verification. Android downloads Qwen and Whisper weights, roughly 499 MiB combined; iOS downloads Qwen, roughly 469 MiB, and uses supported on-device Apple speech recognition. Allow at least 650 MiB of free space beyond the app. Android can resume downloads; the current iOS downloader restarts interrupted downloads.

The local model can request at most two read-only lookups per question, with at most three notes each. No paid inference service is used by the preview. It exposes no memory writes, Team workspace actions, remote coding terminals or background agent execution. The small model can be slow or wrong; inspect the dated evidence behind an answer.

## Build and verification limits

| Platform | Preview requirement | Evidence and remaining checks |
| --- | --- | --- |
| Android | arm64 Android 9+; development build | Compilation, packaging and selected synthetic device checks exist. Interactive browser sign-in, complete first-run downloads and voice quality still need verification. |
| iOS | iOS 16.4+; Xcode signing for your device | Shared source/runtime checks and unsigned device compilation exist. Physical iPhone sign-in, Keychain, microphone and full chat round trips remain unverified. The pinned inference framework has no iOS Simulator slice. |

See the [Android build guide](https://github.com/tavric-labs/cully/blob/main/apps/android/README.md), [iOS build guide](https://github.com/tavric-labs/cully/blob/main/apps/ios/README.md) and dated [verification record](https://github.com/tavric-labs/cully/blob/main/apps/VERIFICATION.md). Source compilation does not establish production retrieval quality or model accuracy. The [mobile product direction](/mobile-product-direction) describes later work separately.
