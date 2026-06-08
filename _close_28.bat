@echo off
gh issue close 28 --repo RonenDotan/whatsapp-claude-bridge --comment "Implemented in commit 2433f2b: added Invoke-VerifyAndSummary as Step 4 in install.ps1. Prompts user to launch, polls port 8080 via TCP (up to 60s), prints installation summary on success."
echo.
echo Done. Press any key to close.
pause
