@echo off
cd /d "%~dp0"

echo === Clearing lock files ===
del /f .git\index.lock 2>nul
del /f .git\HEAD.lock 2>nul
del /f ".git\refs\heads\feature\15.lock" 2>nul
del /f .git\objects\maintenance.lock 2>nul

echo === Reset bad commit (local only, remote is fine) ===
git reset --hard dc08593d2c0b1f038a868ff6c545be9cb058fa1b

echo === Commit the 5 interface files ===
cd whatsapp-bridge
git add interfaces.go whatsapp_channel.go signal_channel.go claude_llm.go codex_llm.go
git commit -m "feat(#15): add Channel/LLM interfaces and stub implementations"

echo === Push ===
git push myfork feature/15

echo === Done ===
