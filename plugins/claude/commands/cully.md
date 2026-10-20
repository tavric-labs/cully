---
description: Manage Cully — status, suggestions and apply
argument-hint: "[status | suggestions | apply <n>]"
allowed-tools: Bash
---

Run the cully control below, then explain the output plainly to the user:
summarize what each section means, call out anything in the warning/caution
colors first, and if they asked to `apply <n>` state exactly what changed.

!`CULLY_BIN=cully sh -c 'if [ "$#" -eq 0 ]; then exec "$CULLY_BIN" status; else CULLY_ASSUME_YES=1 exec "$CULLY_BIN" "$@"; fi' cully $ARGUMENTS 2>&1`
