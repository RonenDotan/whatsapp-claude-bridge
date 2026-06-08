@echo off
cd /d "%~dp0"

echo === Fast-forward main to installer ===
git checkout main
git merge installer

echo === Set version to 0.18.0 (ticket 18, build 0) ===
echo 0.18.0> whatsapp-bridge\VERSION

echo === Commit version bump ===
git add whatsapp-bridge/VERSION
git commit -m "chore: start ticket #18 — set version to 0.18.0"

echo === Push main ===
git push myfork main

echo === Tag v0.18-pre-merge as rollback point ===
git tag v0.18-pre-merge
git push myfork v0.18-pre-merge

echo === Done ===
