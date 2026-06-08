@echo off
cd /d "%~dp0"

echo === Switching to main ===
git checkout main

echo === Tagging current main as v0.14.0-baseline ===
git tag v0.14.0-baseline
git push myfork v0.14.0-baseline

echo === Committing version files ===
git add whatsapp-bridge/VERSION
git add whatsapp-bridge/version.go
git add whatsapp-bridge/main.go
git add whatsapp-bridge/build.bat
git add whatsapp-bridge/start.ps1
git status

git commit -m "feat: add versioning (0.14.0) with --version flag and build.bat

- VERSION file holds current version (starts at 0.14.0)
- version.go declares Version var injected at build time via ldflags
- build.bat increments patch, builds with -ldflags -X main.Version
- --version / version / -v flag prints version and exits
- start.ps1 uses VERSION file when whatsmeow auto-rebuild triggers"

git push myfork main

echo === Done ===
