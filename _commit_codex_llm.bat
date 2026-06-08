@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
if exist .git\index.lock del /f .git\index.lock
if exist .git\HEAD.lock del /f .git\HEAD.lock
git add codex_llm.go
git commit -m "feat(#15): implement CodexLLM.Process and ProcessWithAttachment"
git push origin feature/15
echo Done.
pause
