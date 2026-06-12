## Problem

All incoming Signal messages fail with a NullPointerException:

```
Exception: getServerGuid(...) must not be null (NullPointerException)
```

This happens in `signal-service-java-2.15.3_unofficial_147.jar` inside `SignalServiceContent$Companion.class`. The Kotlin compiler generates a null assertion (`checkNotNullExpressionValue`) for `getServerGuid()`, but Signal servers do not always populate the server GUID field — causing every received message to throw NPE before it can be processed.

The bug exists in both `_unofficial_140` (signal-cli 0.14.1) and `_unofficial_147` (signal-cli 0.14.4.1). Re-linking the account as a secondary device does not fix it. It is a library code bug, not an account state issue.

## Root Cause

In `SignalServiceContent$Companion.class`, Kotlin-compiled bytecode contains 10 occurrences of this null assertion sequence:

```
DUP
LDC_W "getServerGuid(...)"
INVOKESTATIC kotlin/jvm/internal/Intrinsics.checkNotNullExpressionValue
```

When `getServerGuid()` returns null (which it does for all messages in this setup), the Kotlin intrinsic throws NPE with the message `getServerGuid(...) must not be null`.

## Fix

`_patch_signal_jar.py` — standalone Python script, no external dependencies:

- Parses the constant pool of `SignalServiceContent$Companion.class` to locate the exact byte pattern
- Replaces all 10 occurrences of the 7-byte null-check sequence with NOP bytes
- Skips the patch if the pattern is not found (bug fixed upstream)
- Logs a warning when a patch is applied, info when skipped

## start.ps1 Changes

- `$SignalCliAutoUpdate` flag added (default: `$false`)
- When `$false`: always installs pinned version `0.14.4.1` — the known-working patched version. On fresh download, `_patch_signal_jar.py` runs automatically.
- When `$true`: checks GitHub for the latest signal-cli release, downloads if newer, then runs the patch check — applies fix if bug still present, skips if fixed upstream.
- Java 25 (`JAVA_HOME` override to Eclipse Adoptium JDK 25) is required for signal-cli 0.14.4.1.

## Current State

signal-cli 0.14.4.1 is pinned and patched. Signal messages route to Claude correctly. The patch is fully automated — no manual steps required on fresh installs or when triggering an update.
