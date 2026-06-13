# WhatsApp & Signal ↔ Claude / Codex Bridge

An autonomous AI assistant bridge for WhatsApp and Signal, built on top of [whatsapp-mcp](https://github.com/lharries/whatsapp-mcp). Incoming messages are routed to Claude CLI or Codex CLI per allowed chat, with persistent sessions, media support, per-chat personalities, and real-time two-way messaging.

**Repo:** https://github.com/RonenDotan/whatsapp-claude-bridge

---

## What it does

- Incoming WhatsApp and Signal messages are routed to Claude or Codex autonomously
- Each chat gets its own persistent LLM session with memory across messages
- Images, documents, and voice messages are downloaded and passed to the LLM
- Voice messages are transcribed (via Whisper) before being sent
- Each chat can have its own personality preset (default, kids, pro, creative)
- Per-chat working directories let Claude/Codex read and write files scoped to each conversation
- The original MCP server (`whatsapp-mcp-server/`) is still included for tool-based access from Claude Desktop

---

## Architecture

### High-Level Diagram

```
                    ┌─────────────────────────────────┐
                    │         WhatsApp Servers         │
                    └────────────────┬────────────────┘
                                     │ WebSocket (whatsmeow)
                                     ▼
┌──────────────────────────────────────────────────────────────┐
│                        Go Bridge                             │
│                                                              │
│  ┌──────────────┐   ┌──────────────┐   ┌─────────────────┐  │
│  │  whatsapp.go │   │  signal.go   │   │    shared.go    │  │
│  │  (WA events) │   │ (Signal evts)│   │ (session mgmt,  │  │
│  └──────┬───────┘   └──────┬───────┘   │  routing, LLM   │  │
│         │                  │           │  dispatch)      │  │
│         └────────┬─────────┘           └────────┬────────┘  │
│                  │                              │            │
│                  ▼                              │            │
│         ┌────────────────┐                     │            │
│         │  Route message │◀────────────────────┘            │
│         │  (allowed chat?│                                   │
│         │   Claude/Codex)│                                   │
│         └───────┬────────┘                                   │
│                 │                                            │
│        ┌────────┴────────┐                                   │
│        ▼                 ▼                                   │
│  ┌───────────┐    ┌────────────┐                             │
│  │claude_llm │    │codex_llm   │                             │
│  │   .go     │    │   .go      │                             │
│  └─────┬─────┘    └─────┬──────┘                            │
└────────┼────────────────┼────────────────────────────────────┘
         │                │
         ▼                ▼
   Claude CLI          Codex CLI
   (subprocess)        (subprocess)
         │                │
         └────────┬────────┘
                  │ HTTPS
                  ▼
         ┌─────────────────┐
         │  Anthropic API  │
         │  /  OpenAI API  │
         └─────────────────┘
```

Signal connects via a separate path:

```
Signal Network ──► signal-cli daemon (TCP :7583) ──► signal.go (Go bridge)
```

### Message Flow

```
Incoming message (WhatsApp or Signal)
        │
        ├─ Skip: fromMe, status broadcasts
        ├─ Bridge commands (!help, !allow, !set, !clear …) → handled inline
        │
        ▼
isAllowedChat?  ──No──▶  ignore
        │ Yes
        ▼
isCodexChat?
   Yes ─┤─ No
   ▼    ▼
Codex  Claude
        │
        ▼
handleWithClaude / handleWithCodex
        │
        ├─ ensureChatDir → bridge-data/chats/<id>/
        ├─ getPersonalityPrompt → prepend to first message
        ├─ anti-loop check
        ├─ spawn Claude/Codex CLI with --resume <session_id>
        ├─ parse reply + new session_id
        ├─ saveSession
        └─ send reply back to WhatsApp / Signal
```

### Components

| Component | Description |
|-----------|-------------|
| `whatsapp-bridge/` | Go binary — the core bridge (builds to `whatsapp-bridge.exe`) |
| `whatsapp-mcp-server/` | Python MCP server — exposes WhatsApp as MCP tools for Claude Desktop |
| `bridge-data/` | Runtime user state — sessions, allowed chats, per-chat dirs (outside repo) |
| signal-cli | External Java daemon — Go bridge connects via TCP JSON-RPC |

### Go Bridge Source Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point; loads settings, starts WhatsApp and/or Signal goroutines |
| `whatsapp.go` | WhatsApp event handler, message routing, REST API on `:8080` |
| `whatsapp_channel.go` | Media download pipeline for WhatsApp attachments |
| `signal.go` | Signal event handler (TCP connection to signal-cli) |
| `signal_channel.go` | Media download pipeline for Signal attachments |
| `shared.go` | Session management, allowed-chat logic, LLM dispatch, path helpers, anti-loop detection |
| `claude_llm.go` | `ClaudeLLM` — wraps Claude CLI invocation for the `LLM` interface |
| `codex_llm.go` | `CodexLLM` — wraps Codex CLI invocation for the `LLM` interface |
| `interfaces.go` | `LLM` and `Channel` interfaces; `IncomingMessage`, `Attachment` types |
| `personalities.go` | Per-chat personality preset management; writes per-chat `CLAUDE.md`/`AGENTS.md` |
| `settings.go` | Runtime settings (WhatsApp/Signal enabled flags) |
| `version.go` | Version string injected at build time |

### Directory Layout

```
whatsapp-mcp/                    ← git repo root
  whatsapp-bridge/               ← Go source + compiled binary
    config/                      ← committed to git, read-only at runtime
      reaction_prompts.json
      templates/
        personalities/           ← personality prompt templates (*.md)
        settings/
          settings.local.json    ← template for per-chat .claude/ dirs
    store/                       ← NOT in git; whatsmeow DBs only
      messages.db
      whatsapp.db

  bridge-data/                   ← NOT in git; all runtime user state
    sessions.json                ← Claude session IDs per chat
    codex_sessions.json          ← Codex session IDs per chat
    allowed_chats.json           ← WhatsApp chats routed to Claude
    codex_allowed_chats.json     ← WhatsApp chats routed to Codex
    signal_allowed_chats.json    ← Signal chats routed to Claude
    signal_codex_allowed_chats.json
    chat_personalities.json
    settings.json
    chats/
      <chatJID>/                 ← per-chat working directory
        CLAUDE.md or AGENTS.md   ← personality context
        .claude/
          settings.local.json    ← bypassPermissions for Claude CLI
        media/                   ← downloaded attachments
        cache.json               ← recent message cache (anti-loop)

  whatsapp-mcp-server/           ← Python MCP server
```

### LLM Invocation

**Claude CLI:**
```
claude -p \
  --output-format stream-json \
  --input-format stream-json \
  --resume <session_id>
```
Prompt sent via stdin as a stream-json message (supports text + base64 image blocks). Session ID persisted to `bridge-data/sessions.json`. Working directory: `bridge-data/chats/<chatJID>/`.

**Codex CLI:**
```
codex exec \
  --json \
  --output-last-message <tmpfile> \
  --skip-git-repo-check \
  --dangerously-bypass-approvals-and-sandbox \
  -s workspace-write \
  [--resume <session_id>]
```
Session ID persisted to `bridge-data/codex_sessions.json`. Working directory: `bridge-data/chats/<chatJID>/`.

---

## Prerequisites (Windows)

- Go (with CGO enabled — requires a C compiler)
- [MSYS2](https://www.msys2.org/) — install, then add `ucrt64\bin` to your PATH
- Python 3.6+ and [uv](https://github.com/astral-sh/uv) (for whatsapp-mcp-server)
- Claude CLI (`claude`) installed and authenticated
- Codex CLI (`codex`) installed and authenticated (if using Codex routing)
- [signal-cli](https://github.com/AsamK/signal-cli) (optional, for Signal support)
- FFmpeg (optional, for audio transcription via Whisper)

---

## Setup

### 1. Clone

```bash
git clone https://github.com/RonenDotan/whatsapp-claude-bridge.git
cd whatsapp-claude-bridge
```

### 2. Install

```bat
cd whatsapp-bridge
install.bat
```

This sets the `WHATSAPP_BRIDGE_DATA_DIR` environment variable, creates `bridge-data/`, and initialises default settings.

### 3. Build and start

```bat
build.bat
start.bat
```

On first run, a QR code will appear — scan it with your WhatsApp mobile app to authenticate.

### 4. Allow chats

Use bridge commands from any allowed WhatsApp chat, or edit `bridge-data/allowed_chats.json` directly:

```json
["972501234567@s.whatsapp.net", "120363xxxxxx@g.us"]
```

| File | Routes to |
|------|----------|
| `allowed_chats.json` | Claude (WhatsApp) |
| `codex_allowed_chats.json` | Codex (WhatsApp) |
| `signal_allowed_chats.json` | Claude (Signal) |
| `signal_codex_allowed_chats.json` | Codex (Signal) |

---

## Running and Restarting

```bat
start.bat              # restart all components
start.bat bridge       # restart Go bridge only
start.bat signal       # restart signal-cli only
start.bat whatsapp     # restart whatsapp-mcp-server only
```

**Never** launch `whatsapp-bridge.exe` directly — always use `start.bat`. Direct launch bypasses the kill-before-start logic and creates conflicting instances.

Logs are rotated on each restart (5 copies kept):
- `bridge.log` / `bridge.err` — Go bridge stdout/stderr
- `signal-cli.log` — Signal CLI output

---

## Personalities

Each chat can have a personality preset:

| Preset | Description |
|--------|-------------|
| `default` | Standard assistant |
| `kids` | Child-friendly, Hebrew/English, tuned Whisper config |
| `pro` | Professional tone |
| `creative` | Creative/playful |

Templates live in `config/templates/personalities/`. On first message to a chat, the preset is written to `bridge-data/chats/<chatJID>/CLAUDE.md` (Claude) or `AGENTS.md` (Codex), which the LLM reads as its system context.

---

## Media Support

- **Images and documents** — downloaded via `DownloadAny` and passed to the LLM with the message
- **Voice messages** — downloaded, converted, transcribed via Whisper, prepended as `[🎤 Voice]: <transcript>`
- **Sending** — the REST API (`POST /api/send`) accepts a `media_path` field for outbound media

---

## Versioning

Format: `0.<ticket>.<build>` — e.g. `0.41.6` = ticket 41, sixth build.

Version is stored in `VERSION` and injected at build time via `-ldflags "-X main.Version=X.Y.Z"`. Always build with `build.bat` — it auto-bumps the build number.

---

## Original MCP Server

The `whatsapp-mcp-server/` Python MCP server from the upstream project is still included. It connects to the Go bridge's REST API and exposes WhatsApp as MCP tools for Claude Desktop or Cursor. See the [original README](https://github.com/lharries/whatsapp-mcp) for setup.
