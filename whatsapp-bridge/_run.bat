@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
(
  git add -A
  git commit -m "feat(#38): restore test path message alongside real media send (v0.30.6)"
  git push
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
echo === done === >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt
