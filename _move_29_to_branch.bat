@echo off
cd /d "C:\Users\ronen\whatsapp-mcp"

echo Step 1: Create branch feature/29 at current HEAD (includes the #29 commit)...
git checkout -b feature/29
if %ERRORLEVEL% NEQ 0 ( echo FAILED to create branch & pause & exit /b 1 )

echo Step 2: Switch back to installer...
git checkout installer
if %ERRORLEVEL% NEQ 0 ( echo FAILED to checkout installer & pause & exit /b 1 )

echo Step 3: Revert the #29 commit from installer...
git -c user.email="ronendotan@gmail.com" -c user.name="Ronen Dotan" revert a24d6ca --no-edit
if %ERRORLEVEL% NEQ 0 ( echo FAILED to revert & pause & exit /b 1 )

echo Step 4: Push installer (with revert)...
git push myfork installer
if %ERRORLEVEL% NEQ 0 ( echo FAILED to push installer & pause & exit /b 1 )

echo Step 5: Push feature/29...
git push myfork feature/29
if %ERRORLEVEL% NEQ 0 ( echo FAILED to push feature/29 & pause & exit /b 1 )

echo.
echo All done.
pause
