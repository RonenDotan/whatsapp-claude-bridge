@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge

echo Stopping bridge...
powershell -NoProfile -ExecutionPolicy Bypass -Command "Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*whatsapp-bridge.exe*' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
timeout /t 2 /nobreak >nul

echo Building v0.40.12...
go build -ldflags "-X main.Version=0.40.12" -o whatsapp-bridge.exe .
if %ERRORLEVEL% neq 0 (
    echo BUILD FAILED
    exit /b 1
)
echo BUILD OK

echo Starting bridge...
powershell -NoProfile -ExecutionPolicy Bypass -File "C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\start.ps1" bridge

echo Done.
