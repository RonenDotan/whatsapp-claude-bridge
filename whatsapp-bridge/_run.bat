@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
REM ── Commands to run ──────────────────────────────────────────────────────────
(
  git add -A
  git commit -m "feat(#38): stage 4 test — send output file path as text message (v0.30.4)"
  git push
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
echo === done === >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt
REM ─────────────────────────────────────────────────────────────────────────────
