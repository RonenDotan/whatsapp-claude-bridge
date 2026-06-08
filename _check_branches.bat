@echo off
cd /d "C:\Users\ronen\whatsapp-mcp"
echo === branches === > C:\Users\ronen\whatsapp-mcp\_branches_out.txt
git branch -a >> C:\Users\ronen\whatsapp-mcp\_branches_out.txt
echo === installer log === >> C:\Users\ronen\whatsapp-mcp\_branches_out.txt
git log installer --oneline -5 >> C:\Users\ronen\whatsapp-mcp\_branches_out.txt
echo === feature/29 log === >> C:\Users\ronen\whatsapp-mcp\_branches_out.txt
git log feature/29 --oneline -5 >> C:\Users\ronen\whatsapp-mcp\_branches_out.txt
echo Done. Press any key to close.
pause
