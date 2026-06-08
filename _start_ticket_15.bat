@echo off
cd /d "%~dp0"

echo === Switch to main and pull latest ===
cd whatsapp-bridge
git checkout main
git pull myfork main

echo === Create feature/15 branch ===
git checkout -b feature/15

echo === Set VERSION to 0.15.0 ===
echo 0.15.0> VERSION

echo === Commit version bump ===
git add VERSION
git commit -m "chore: start ticket #15 — set version to 0.15.0"

echo === Push branch ===
git push myfork feature/15

echo === Done — on feature/15 at 0.15.0 ===
