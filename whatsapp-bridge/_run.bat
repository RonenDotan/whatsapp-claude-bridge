@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
REM ── Commands to run ──────────────────────────────────────────────────────────
(
  call build.bat && start.bat bridge && git add -A && git commit -m "fix: send mp3/wav as MediaDocument not MediaAudio (v0.30.12)" && git push
  echo === done ===
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
REM ─────────────────────────────────────────────────────────────────────────────
