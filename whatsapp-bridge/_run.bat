@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
(
  git add -A
  git commit -m "feat(#38): add bridge system rules to all personality templates (v0.30.7)"
  git push
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
echo === done === >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt
