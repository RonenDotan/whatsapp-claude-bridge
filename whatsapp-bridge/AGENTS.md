# WhatsApp Claude Bridge — Dev Context (Codex)

You are Codex, assisting with development of the whatsapp-claude-bridge project.

## Project Overview

Autonomous AI assistant bridge for WhatsApp and Signal. Routes incoming messages to Claude CLI or Codex CLI per allowed chat. Built on top of whatsapp-mcp.

**Repo:** https://github.com/RonenDotan/whatsapp-claude-bridge

## Architecture

```
WhatsApp ──► Go Bridge ──► Claude CLI  (per allowed chat)
                      └──► Codex CLI  (per codex-allowed chat)
Signal   ──► Go Bridge ──► Claude CLI  (per allowed signal chat)
                      └──► Codex CLI  (per signal-codex-allowed chat)
```

## Source Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, settings init, starts bridge components |
| `whatsapp.go` | WhatsApp event handler, message routing, REST API |
| `whatsapp_channel.go` | Media download pipeline |
| `shared.go` | Claude CLI invocation, session management, allowed-chat logic |
| `claude_llm.go` | Claude LLM integration |
| `codex_llm.go` | Codex LLM integration |
| `interfaces.go` | Shared `LLM` interface |
| `personalities.go` | Per-chat personality presets; writes per-chat CLAUDE.md/AGENTS.md to `store/chats/<chatJID>/` |
| `settings.go` | Runtime settings (WhatsApp/Signal enabled) |
| `signal.go` | Signal event handler |
| `signal_channel.go` | Signal media pipeline |
| `version.go` | Version variable (injected at build time) |

## Runtime Data (`store/`)

| Path | Purpose |
|------|---------|
| `store/messages.db` | WhatsApp message history |
| `store/whatsapp.db` | whatsmeow device/session store |
| `store/sessions.json` | Claude session IDs per chat |
| `store/codex_sessions.json` | Codex session IDs per chat |
| `store/allowed_chats.json` | Chats routed to Claude |
| `store/codex_allowed_chats.json` | Chats routed to Codex |
| `store/signal_allowed_chats.json` | Signal chats routed to Claude |
| `store/signal_codex_allowed_chats.json` | Signal chats routed to Codex |
| `store/chat_personalities.json` | Per-chat personality preset |
| `store/templates/` | Embedded personality prompt templates |
| `store/chats/<chatJID>/` | Per-chat dir: CLAUDE.md or AGENTS.md, session context, media files |

## Per-Chat Context Files vs Project Root

`personalities.go` writes personality prompts to `store/chats/<chatJID>/AGENTS.md` (for Codex chats) or CLAUDE.md.
The files `whatsapp-bridge/CLAUDE.md` and `whatsapp-bridge/AGENTS.md` at project root are **dev context for you** — they are NOT touched by the bridge at runtime.

## Versioning

Format: `0.<ticket>.<build>` — e.g. `0.30.2` = ticket 30, second build.
- Version stored in `VERSION` file
- Injected at build time via `-ldflags "-X main.Version=X.Y.Z"`
- Always bump the build number in both `VERSION` and `_stop_build_start.bat` before a new build

## Build Rules — CRITICAL

**ALWAYS use `build.bat`** to compile. NEVER run `go build` directly.
- `build.bat` handles CGO setup, PATH (MSYS2 gcc), ldflags, and version injection
- Running `go build` directly will likely fail (missing CGO) or produce a binary without the version

## Restart Rules — CRITICAL

**NEVER launch `whatsapp-bridge.exe` directly.**
**ALWAYS use `start.bat`** or one of:

```
start.bat              # restart all components
start.bat bridge       # restart Go bridge only
start.bat signal       # restart signal-cli only
start.bat whatsapp     # restart whatsapp-mcp-server only
```

Direct launch bypasses the kill-before-start logic and creates conflicting instances.

## Git Workflow — CRITICAL

The bash sandbox has a corrupted git HEAD (null bytes) — bash git is unreliable for write ops.

**Rules:**
1. Use bash for read-only ops (git log, git status, git diff) — usually works
2. For ALL write ops (commit, push, gh CLI): write commands to `_run.bat`, run via Win+R
3. NEVER create one-off batch files — use the single reusable `_run.bat`

**`_run.bat` pattern:**
1. Write commands into `_run.bat` (redirect output → absolute path, see below)
2. Open Win+R (`key: "win+r"`)
3. `ctrl+a` to select all in Run field, then type: `C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run.bat`
4. Press Enter, wait 6–10 seconds
5. Read `_run_out.txt` for results
6. Reset `_run.bat` to placeholder after use

**Output redirect — CRITICAL:** Always use an absolute path for the output file.
In grouped commands `( ... ) > output.txt`, the redirect resolves BEFORE `cd` runs inside the group,
so a relative path writes to the wrong directory. Always use:
```
( ... ) > C:\Users\ronen\whatsapp-mcp\whatsapp-bridge\_run_out.txt 2>&1
```

**Win+R tips:**
- Always `ctrl+a` before typing — prevents appending to old path
- Use Win+R, NOT File Explorer double-click (Explorer address bar triggers TextInputHost)

## Computer Use

**TextInputHost** (Windows touch keyboard) steals foreground when any text field is focused.
Request it explicitly at every session start:
```
request_access(apps=["File Explorer", "textinputhost.exe"], reason="...")
```

## Key Implementation Notes

- `whatsapp.go` uses `fmt.Printf` for logging — does NOT import the `"log"` package
- Use `client.DownloadAny(ctx, msg.Message)` for media download, not `client.Download()` with custom struct
- Codex sessions run with the working dir set to `store/chats/<chatJID>/` — reads AGENTS.md from per-chat dir
- Logs rotate on restart (5 copies kept): `bridge.log`, `bridge.err`, `signal-cli.log`
