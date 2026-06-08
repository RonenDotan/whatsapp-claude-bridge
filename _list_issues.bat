@echo off
gh issue list --repo RonenDotan/whatsapp-claude-bridge --state open --json number,title,labels > C:\Users\ronen\whatsapp-mcp\_issues_out.txt 2>&1
echo Done. Press any key to close.
pause
