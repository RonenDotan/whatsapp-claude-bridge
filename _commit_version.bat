@echo off
cd /d "%~dp0"

echo === Committing VERSION 0.14.1 to main ===
git checkout main
git add whatsapp-bridge/VERSION
git commit -m "chore: bump version to 0.14.1"
git push myfork main

echo === Tagging as v0.14 ===
git tag v0.14
git push myfork v0.14

echo === Done ===
