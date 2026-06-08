@echo off
cd /d "%~dp0"
del /f .git\index.lock 2>nul
git add whatsapp-bridge/install.ps1
git -c user.email="ronendotan@gmail.com" -c user.name="Ronen Dotan" commit -m "feat(installer): add Step 4 launch verification and summary (#28)"
echo.
echo Done. Press any key to close.
pause
