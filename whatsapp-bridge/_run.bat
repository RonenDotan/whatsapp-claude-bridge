@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
REM ── Commands to run ──────────────────────────────────────────────────────────
(
  git add -A && git commit -m "feat(#38): cancel command, 10min timeout, 3min working nudge (v0.30.9)" && git push
  echo === done ===
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
REM ─────────────────────────────────────────────────────────────────────────────
