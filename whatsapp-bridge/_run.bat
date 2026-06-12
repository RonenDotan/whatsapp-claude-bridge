@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
git add -A >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git status >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git commit -m "fix: migrate data to bridge-data; fix fallback dirs in llm files; set WHATSAPP_BRIDGE_DATA_DIR in start.ps1" >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git push origin main >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
