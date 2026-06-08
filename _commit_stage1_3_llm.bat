@echo off
cd /d C:\Users\ronen\whatsapp-mcp\whatsapp-bridge

:: Remove any stale lock files
if exist .git\index.lock del /f .git\index.lock
if exist .git\HEAD.lock del /f .git\HEAD.lock

git add interfaces.go whatsapp_channel.go signal_channel.go claude_llm.go codex_llm.go

git commit -m "feat(#15): stage1/2/3 - interfaces, channel stubs, ClaudeLLM.ProcessWithAttachment

- interfaces.go: Channel + LLM interfaces, IncomingMessage, Attachment types
- whatsapp_channel.go: ReceiveAttachment (downloadMedia, skip audio) + SendMessage
- signal_channel.go: ReceiveAttachment (resolve signalAttachmentsDir, skip audio) + SendMessage
- claude_llm.go: Process (wraps handleWithClaude) + ProcessWithAttachment (--image flag, session resume/retry)
- codex_llm.go: Process stub + ProcessWithAttachment graceful unsupported error"

git push origin feature/15
echo Done.
pause
