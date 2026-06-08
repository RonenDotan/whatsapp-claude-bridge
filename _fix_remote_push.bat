@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge

echo Current remote:
git remote -v

echo.
echo Fixing remote URL...
git remote set-url origin https://github.com/RonenDotan/whatsapp-claude-bridge.git

echo.
echo New remote:
git remote -v

echo.
echo Pushing feature/15...
git push origin feature/15

echo.
echo Done.
pause
