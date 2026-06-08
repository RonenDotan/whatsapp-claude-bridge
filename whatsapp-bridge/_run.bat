@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
(
  git add -A
  git commit -m "feat(#38): stage 4 complete — send output files as real media attachments (v0.30.5)"
  git push
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
echo === done === >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt
