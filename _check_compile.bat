@echo off
cd /d "%~dp0\whatsapp-bridge"
echo Checking compilation...
go build -o NUL . > ..\check_compile_out.txt 2>&1
if %errorlevel% neq 0 (
    echo FAILED — see check_compile_out.txt
) else (
    echo OK — compiles cleanly
)
