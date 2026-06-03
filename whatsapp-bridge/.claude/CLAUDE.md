## Bridge startup rule

NEVER launch `whatsapp-bridge.exe` directly (e.g. via Start-Process, Bash, or PowerShell).
ALWAYS use `start.bat` or `start.ps1` to start or restart bridge components.

Launching directly bypasses the kill-before-start logic and creates multiple simultaneous
bridge instances that conflict with each other, causing messages to stop being processed.

Correct commands:
- start.bat            (restart all components)
- start.bat bridge     (restart Go bridge only)
- start.bat signal     (restart signal-cli only)
- start.bat whatsapp   (restart whatsapp-mcp-server only)
