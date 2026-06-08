@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
if exist .git\index.lock del /f .git\index.lock
if exist .git\HEAD.lock del /f .git\HEAD.lock
git add whatsapp.go signal.go VERSION
git commit -m "feat(#15): stage4 - wire attachment pipeline into message handlers (v0.15.1)"
git push origin feature/15
echo Done.
pause
