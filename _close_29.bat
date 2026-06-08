@echo off
gh issue close 29 --repo RonenDotan/whatsapp-claude-bridge --comment "Implemented: bridge now creates <chatDir>/.claude/settings.local.json from template on !meet-claude/!meet-codex. Template lives at .claude/templates/settings.local.json (bypassPermissions). Committed a24dbca."
echo.
echo Done. Press any key to close.
pause
