# WhatsApp Claude Bridge

An autonomous AI assistant bridge for WhatsApp (and Signal), built on top of the [whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) project. Instead of just exposing WhatsApp data to an LLM tool, this bridge turns Claude and Codex into live WhatsApp assistants — they read incoming messages and reply autonomously, per chat.

**GitHub:** https://github.com/RonenDotan/whatsapp-claude-bridge

---

## What it does

- Incoming WhatsApp (and Signal) messages are routed to Claude or Codex
- Each chat gets its own persistent Claude/Codex session with memory
- Images, files, and voice messages are downloaded and passed to the LLM
- Voice messages are transcribed (via Whisper) before being sent to the LLM
- Each chat can have its own personality preset (default, kids, pro, creative)
- The original MCP server (`whatsapp-mcp-server/`) is still included for tool-based access

---

## Architecture

```
WhatsApp ──► Go Bridge ──► Claude CLI  (per allowed chat)
                      └──► Codex CLI  (per codex-allowed chat)
Signal   ──► Go Bridge ──► Claude CLI  (per allowed signal chat)
                      └──► Codex CLI  (per signal-codex-allowed chat)

whatsapp-mcp-server/ ──► MCP tools for Claude Desktop (original functionality, still available)
```

### Components

| Component | Description |
|-----------|-------------|
| `whatsapp-bridge/` | Go binary — the core bridge |
| `whatsapp-mcp-server/` | Python MCP server (original upstream feature) |

### Go bridge source files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, settings init, starts bridge components |
| `whatsapp.go` | WhatsApp event handler, message routing, REST API |
| `whatsapp_channel.go` | Media download pipeline (`WhatsAppChannel`) |
| `shared.go` | Claude CLI invocation, session management, allowed-chat logic |
| `claude_llm.go` | Claude LLM integration |
| `codex_llm.go` | Codex LLM integration |
| `interfaces.go` | Shared `LLM` interface |
| `personalities.go` | Per-chat personality presets, writes per-chat CLAUDE.md/AGENTS.md |
| `settings.go` | Runtime settings (WhatsApp/Signal enabled) |
| `signal.go` | Signal event handler |
| `signal_channel.go` | Signal media pipeline |
| `version.go` | Version variable (set at build time) |

### Runtime data (`store/`)

| Path | Purpose |
|------|---------|
| `store/messages.db` | WhatsApp message history (SQLite) |
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

---

## Prerequisites (Windows)

- Go (with CGO enabled — requires a C compiler)
- [MSYS2](https://www.msys2.org/) — install, then add `ucrt64\bin` to your PATH
- Python 3.6+ and [uv](https://github.com/astral-sh/uv) (for whatsapp-mcp-server)
- [signal-cli](https://github.com/AsamK/signal-cli) (optional, for Signal support)
- FFmpeg (optional, for audio conversion)
- Claude CLI (`claude`) installed and authenticated
- Codex CLI (`codex`) installed and authenticated (if using Codex routing)

---

## Setup

### 1. Clone

```bash
git clone https://github.com/RonenDotan/whatsapp-claude-bridge.git
cd whatsapp-claude-bridge
```

### 2. Build and start

```bat
cd whatsapp-bridge
build.bat
start.bat
```

On first run, a QR code will appear — scan it with your WhatsApp mobile app to authenticate.

### 3. Allow chats

Add chat JIDs to the appropriate allow-list files in `store/`:
- `allowed_chats.json` — chats Claude will respond to
- `codex_allowed_chats.json` — chats Codex will respond to

Format: `["972501234567@s.whatsapp.net", "120363xxxxxx@g.us"]`

---

## Running and restarting

```bat
start.bat              # restart all components
start.bat bridge       # restart Go bridge only
start.bat signal       # restart signal-cli only
start.bat whatsapp     # restart whatsapp-mcp-server only
```

**Never** launch `whatsapp-bridge.exe` directly — always use `start.bat`. Direct launch bypasses the kill-before-start logic and creates conflicting instances.

Logs are rotated on each restart and kept for 5 cycles:
- `bridge.log` / `bridge.err` — Go bridge stdout/stderr
- `signal-cli.log` — Signal CLI output

---

## Media support

- **Images and documents**: downloaded via `DownloadAny` and passed to the LLM with the message
- **Voice messages**: downloaded, transcribed via Whisper, prepended as `[🎤 Voice]: <transcript>`
- **Sending**: the REST API (`POST /api/send`) supports `media_path` for outbound media

---

## Personalities

Each chat can have a personality preset that sets the system prompt:

| Preset | Description |
|--------|-------------|
| `default` | Standard assistant |
| `kids` | Child-friendly, Hebrew/English, tuned Whisper config |
| `pro` | Professional tone |
| `creative` | Creative/playful |

Presets are stored in `store/templates/` and per-chat in `store/chats/<chatJID>/CLAUDE.md` (or `AGENTS.md` for Codex chats).

---

## Versioning

Version format: `0.<ticket>.<build>` — e.g. `0.30.2` = ticket 30, second build.

The version is stored in `VERSION` and embedded at build time via `-ldflags "-X main.Version=X.Y.Z"`.

---

## Original MCP server

The `whatsapp-mcp-server/` Python MCP server from the upstream project is still included. It connects to the Go bridge's REST API and exposes WhatsApp as MCP tools for Claude Desktop or Cursor. See the [original README](https://github.com/lharries/whatsapp-mcp) for setup instructions.
