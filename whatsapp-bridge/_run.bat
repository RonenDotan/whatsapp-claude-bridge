@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
git add -A >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git status >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git commit -m "feat: store refactor, reply context, bridge-data dir (v0.41.5)" >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
git push origin main >> C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
