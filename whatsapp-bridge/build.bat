@echo off
setlocal

cd /d "%~dp0"

:: Read current version
set /p VERSION=<VERSION
echo Current version: %VERSION%

:: Split into parts: MAJOR.MINOR.PATCH
for /f "tokens=1,2,3 delims=." %%a in ("%VERSION%") do (
    set MAJOR=%%a
    set MINOR=%%b
    set PATCH=%%c
)

:: Increment patch
set /a PATCH=%PATCH% + 1
set NEW_VERSION=%MAJOR%.%MINOR%.%PATCH%

:: Write back
echo %NEW_VERSION%> VERSION
echo New version:     %NEW_VERSION%

:: Build
echo Building whatsapp-bridge.exe...
go build -ldflags "-X main.Version=%NEW_VERSION%" -o whatsapp-bridge.exe .
if %errorlevel% neq 0 (
    echo BUILD FAILED — reverting version
    echo %VERSION%> VERSION
    exit /b 1
)

echo Done: whatsapp-bridge.exe v%NEW_VERSION%
endlocal
