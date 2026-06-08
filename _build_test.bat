@echo off
cd /d "C:\Users\ronen\whatsapp-mcp\whatsapp-bridge"
go build -o _testbuild.exe . 2>&1
if %ERRORLEVEL% EQU 0 (
    del _testbuild.exe
    echo BUILD OK
) else (
    echo BUILD FAILED
)
echo.
pause
