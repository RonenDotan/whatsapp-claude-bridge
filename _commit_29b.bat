@echo off
cd /d "C:\Users\ronen\whatsapp-mcp"
del /f .git\index.lock 2>nul
git status
echo ===
git add "whatsapp-bridge/shared.go"
git add "whatsapp-bridge/whatsapp.go"
git add "whatsapp-bridge/signal.go"
git add "whatsapp-bridge/.claude/templates/settings.local.json"
echo After add:
git status --short
echo ===
git -c user.email="ronendotan@gmail.com" -c user.name="Ronen Dotan" commit -m "feat(sessions): copy settings.local.json template to each chat on !meet (#29)"
echo.
echo Done. Press any key to close.
pause
