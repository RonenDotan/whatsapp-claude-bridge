@echo off
setlocal
set "_arg=%~1"
if /i "%_arg%"=="--help" set "_arg=help"
if /i "%_arg%"=="-h"     set "_arg=help"
if /i "%_arg%"=="/?"     set "_arg=help"
if "%_arg%"=="" (powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1") else powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" "%_arg%"
endlocal
