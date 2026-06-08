@echo off
cd /d "C:\Users\ronen\whatsapp-mcp"
git log --oneline -8 > C:\Users\ronen\whatsapp-mcp\_log_out.txt 2>&1
type C:\Users\ronen\whatsapp-mcp\_log_out.txt
echo.
pause
