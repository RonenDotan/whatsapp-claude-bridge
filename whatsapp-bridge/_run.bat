@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
REM ── Commands to run ──────────────────────────────────────────────────────────
(
  call build.bat && start.bat bridge && git add -A && git commit -m "feat(#38): add .mp3/.wav to output whitelist + MIME types (v0.30.10)" && git push
  echo === done ===
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
REM ─────────────────────────────────────────────────────────────────────────────
