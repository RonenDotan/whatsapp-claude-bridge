@echo off
cd /d "%~dp0"

echo === Switch to main ===
git checkout main

echo === Delete installer branch (local + remote) ===
git branch -d installer
git push myfork --delete installer

echo === Re-apply #29 by reverting the revert ===
git revert e550da5 --no-edit

echo === Set version to 0.29.0 (ticket 29) ===
echo 0.29.0> whatsapp-bridge\VERSION

echo === Commit version bump ===
git add whatsapp-bridge\VERSION
git commit -m "chore: start ticket #29 — set version to 0.29.0"

echo === Push main ===
git push myfork main

echo === Done ===
