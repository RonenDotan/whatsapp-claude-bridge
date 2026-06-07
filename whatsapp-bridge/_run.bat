@echo off
cd /d C:\Users\ronen\whatsapp-mcp
git add whatsapp-bridge/whatsapp.go whatsapp-bridge/VERSION whatsapp-bridge/_stop_build_start.bat whatsapp-bridge/.claude/CLAUDE.md whatsapp-bridge/_run.bat whatsapp-bridge/_restart_bridge.bat > _run_out.txt 2>&1
git commit -m "fix(audio): use DownloadAny for voice messages; fix log import (v0.30.2)" >> _run_out.txt 2>&1
git log --oneline -4 >> _run_out.txt 2>&1
type _run_out.txt
