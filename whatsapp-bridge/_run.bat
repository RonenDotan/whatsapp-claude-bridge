@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge
(
  git add -A
  git status
  git commit -m "feat: refactor store layout; add reply-to context; data/config separation

- Move runtime user state to bridge-data/ (configurable via WHATSAPP_BRIDGE_DATA_DIR)
- Add config/ dir for committed templates and reaction_prompts.json (no longer embedded)
- store/ is now fully gitignored; only messages.db + whatsapp.db remain at fixed path
- Add reply-to context: prepend [Replying to: ...] for WhatsApp + Signal quoted messages
- update install.ps1 and start.ps1 to set WHATSAPP_BRIDGE_DATA_DIR default
- Fix fallback chatDir in claude_llm.go and codex_llm.go to use dataDir()
- Bump version to 0.41.5"
  git push origin main
) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
