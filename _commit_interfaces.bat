@echo off
cd /d "%~dp0\whatsapp-bridge"
git add interfaces.go whatsapp_channel.go signal_channel.go claude_llm.go codex_llm.go
git commit -m "feat(#15): add Channel/LLM interfaces and stub implementations"
git push myfork feature/15
echo === Done ===
