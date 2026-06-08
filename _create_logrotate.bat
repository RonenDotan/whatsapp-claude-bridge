@echo off
gh issue create --repo RonenDotan/whatsapp-claude-bridge --project "whatsapp-claude-bridge" ^
  --title "Add log rotation for bridge.log and signal-cli.log" ^
  --label "enhancement" ^
  --body "## Problem^^bridge.log accumulates indefinitely across all restarts with no truncation or rotation. The same applies to signal-cli.log. On always-on systems these files grow without bound.^^## Options^^**Option 1: Rotate on startup (start.ps1 / start.bat)**^^Before launching the bridge, rename existing logs:^^- bridge.log -> bridge.log.1, bridge.log.1 -> bridge.log.2, etc.^^- Keep last N copies (e.g. 5), delete anything older^^- Start fresh with an empty bridge.log^^**Option 2: Size-based rotation**^^Rotate only when file exceeds a threshold (e.g. 10 MB). Same rename-and-keep-N logic.^^**Option 3: Native Go log rotation (bridge.log only)**^^Use a library like lumberjack inside the bridge process. signal-cli.log would still need script-level handling.^^## Expected behavior^^Log files should be capped at a reasonable size or rotated on each restart."
echo.
echo Done. Press any key to close.
pause
