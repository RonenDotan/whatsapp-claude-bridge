@echo off
cd /d "%~dp0"

gh issue create ^
  --repo RonenDotan/whatsapp-claude-bridge ^
  --title "Session permissions via commands" ^
  --body "## Overview^^Allow changing Claude Code permission mode per-chat via a bridge command.^^## Background^^Issue #29 introduced per-chat `.claude/settings.local.json`, seeded from a template on `!meet-claude` / `!meet-codex`. Currently the permission mode is fixed at whatever the template contains (`bypassPermissions` by default).^^## Goal^^Add a command (e.g. `!set-permissions <mode>`) that lets the user change the Claude Code permission mode for a specific chat at runtime, without manually editing the settings file.^^## Modes to support^^- `bypass` — bypassPermissions (current default)^^- `default` — default mode (prompts for approval)^^- `approve` — acceptEdits^^## Implementation notes^^- Read/write the existing `.claude/settings.local.json` for the chat (created by #29)^^- Command available on both WhatsApp and Signal sides^^- Persist immediately so the next Claude/Codex session picks it up^^## Acceptance criteria^^- `!set-permissions bypass` sets defaultMode to bypassPermissions^^- `!set-permissions default` removes the override (or sets to default)^^- `!set-permissions approve` sets defaultMode to acceptEdits^^- Confirmation message returned to the chat^^- Works for both Claude and Codex sessions" ^
  --label "enhancement" ^
  --project "whatsapp-claude-bridge"

echo === Done ===
