# Architecture: WhatsApp & Signal ↔ Claude / Codex Bridge

> **Status:** Fully implemented and running. This issue serves as the living architecture reference.

---

## Overview

An autonomous AI assistant bridge that routes incoming messages from **WhatsApp** and **Signal** to either **Claude CLI** or **Codex CLI**, maintains persistent conversation sessions per chat, and sends replies back. Built on top of [whatsapp-mcp](https://github.com/tulir/whatsmeow).

---

## High-Level Architecture

```
                    ┌─────────────────────────────────┐
                    │         WhatsApp Servers         │
                    └────────────────┬────────────────┘
                                     │ WebSocket (whatsmeow)
                                     ▼
┌──────────────────────────────────────────────────────────────┐
│                      Go Bridge                               │
│                                                              │
│  ┌──────────────┐   ┌──────────────┐   ┌─────────────────┐  │
│  │  whatsapp.go │   │  signal.go   │   │    shared.go    │  │
│  │  (WA events) │   │ (Signal evts)│   │ (session mgmt,  │  │
│  └──────┬───────┘   └──────┬───────┘   │  allowed chats, │  │
│         │                  │           │  LLM dispatch)  │  │
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
│        │                │                                    │
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
Signal Network
      │
      ▼
signal-cli daemon (TCP :7583)
      │
      ▼
signal.go (Go bridge — connects via TCP JSON-RPC)
```

---

## Components

### Go Bridge (`whatsapp-bridge/`)

The core binary. All logic lives here.

| File | Role |
|------|------|
| `main.go` | Entry point — loads settings, starts WhatsApp and/or Signal goroutines |
| `whatsapp.go` | WhatsApp event handler; message routing; REST API on `:8080` |
| `whatsapp_channel.go` | Media download pipeline for WhatsApp attachments |
| `signal.go` | Signal event handler (connects to signal-cli TCP daemon) |
| `signal_channel.go` | Media download pipeline for Signal attachments |
| `shared.go` | Session management, allowed-chat logic, LLM dispatch (`handleWithClaude`, `handleWithCodex`), path helpers, anti-loop detection |
| `claude_llm.go` | `ClaudeLLM` struct — wraps `handleWithClaude` for the `LLM` interface (used for attachment handling) |
| `codex_llm.go` | `CodexLLM` struct — wraps Codex CLI for the `LLM` interface |
| `interfaces.go` | `LLM` and `Channel` interfaces; `IncomingMessage`, `Attachment` types |
| `personalities.go` | Per-chat personality preset management; writes `CLAUDE.md`/`AGENTS.md` to per-chat dirs |
| `settings.go` | Runtime settings (WhatsApp/Signal enabled flags) |
| `version.go` | Version string injected at build time |

### whatsapp-mcp-server (`whatsapp-mcp-server/`)

Python MCP server. Reads `store/messages.db` via SQLite and exposes `mcp__whatsapp__*` tools to Claude Desktop for message history lookups. **Not involved in real-time routing** — that all happens in the Go bridge.

### signal-cli

External Java process. Runs as a daemon on TCP `127.0.0.1:7583`. The Go bridge connects to it at startup and listens for incoming Signal messages over JSON-RPC.

---

## Message Flow

### WhatsApp → LLM → Reply

```
WhatsApp message arrives
        │
        ▼
whatsapp.go: event handler
        │
        ├─ Skip: fromMe, status broadcasts, non-text (unless media enabled)
        ├─ Bridge commands (!help, !allow, !set, !clear, etc.) → handled inline
        │
        ▼
isAllowedChat(jid)?  ──No──▶  ignore
        │ Yes
        ▼
isCodexChat(jid)?
        │
   Yes  │  No
   ▼    ▼
Codex  Claude
        │
        ▼
handleWithClaude / handleWithCodex  (shared.go)
        │
        ├─ ensureChatDir(chatID)  →  bridge-data/chats/<jid>/
        ├─ getPersonalityPrompt(chatID)  →  prepend to first message
        ├─ isLooping(chatID, msg)?  →  abort if anti-loop triggered
        ├─ spawn:  claude -p --output-format stream-json --resume <session_id>
        │          or:  codex exec --json --resume <session_id>
        ├─ parse reply + new session_id
        ├─ saveSession(chatID, sessionID)
        └─ sendFn(reply)  →  POST to WhatsApp HTTP API :8080
```

### Signal → LLM → Reply

Identical routing logic — `signal.go` mirrors `whatsapp.go` but reads from `isSignalAllowedChat` / `isSignalCodexChat` registries, and sends replies via signal-cli TCP.

---

## LLM Invocation

### Claude CLI

```
claude -p \
  --output-format stream-json \
  --input-format stream-json \
  --resume <session_id>          # omit on first message
```

- Prompt sent via **stdin** as a stream-json message (supports text + base64 image blocks for attachments)
- Reply extracted from the `"result"` event in the JSONL stream
- `session_id` from the result event is persisted to `bridge-data/sessions.json`
- Working directory set to `bridge-data/chats/<chatJID>/` so Claude picks up per-chat `CLAUDE.md`

### Codex CLI

```
codex exec \
  --json \
  --output-last-message <tmpfile> \
  --skip-git-repo-check \
  --dangerously-bypass-approvals-and-sandbox \
  -s workspace-write \
  [--resume <session_id>]
```

- Prompt sent via **stdin**
- Reply read from `--output-last-message` temp file
- Session ID parsed from JSONL output
- Persisted to `bridge-data/codex_sessions.json`
- Working directory: `bridge-data/chats/<chatJID>/` → reads per-chat `AGENTS.md`

---

## Session Management

| File | Contents |
|------|----------|
| `bridge-data/sessions.json` | `{ "<chatJID>": "<claudeSessionID>" }` |
| `bridge-data/codex_sessions.json` | `{ "<chatJID>": "<codexSessionID>" }` |

- New chat → spawn without `--resume`, save returned session ID
- Existing chat → spawn with `--resume <session_id>`
- Stale/invalid session → retry once without `--resume` (fresh session)
- `!clear` command → deletes session entry, next message starts fresh

---

## Allowed Chats & Routing

Four independent registries in `bridge-data/`:

| File | Routes to |
|------|----------|
| `allowed_chats.json` | Claude (WhatsApp) |
| `codex_allowed_chats.json` | Codex (WhatsApp) |
| `signal_allowed_chats.json` | Claude (Signal) |
| `signal_codex_allowed_chats.json` | Codex (Signal) |

Managed at runtime via bridge commands (`!allow`, `!deny`, `!codex`, etc.) — changes take effect immediately without restart.

---

## Personality System

Each chat can have a personality preset (default, pro, creative, kids, or custom).

```
personalities.go
      │
      ├─ Templates: config/templates/personalities/*.md
      ├─ Assignment: bridge-data/chat_personalities.json
      │
      └─ On first session start:
             write bridge-data/chats/<chatJID>/CLAUDE.md   (for Claude)
             write bridge-data/chats/<chatJID>/AGENTS.md   (for Codex)
```

Claude/Codex read the context file from the working directory at invocation time.

---

## Directory Layout

```
whatsapp-mcp/                    ← git repo root
  whatsapp-bridge/               ← Go source + compiled binary
    config/                      ← committed to git, read-only at runtime
      reaction_prompts.json
      templates/
        personalities/           ← personality prompt templates
          default.md
          pro.md
          creative.md
          kids.md
        settings/
          settings.local.json    ← template for per-chat .claude/ dirs
    store/                       ← NOT in git; whatsmeow DBs only
      messages.db
      whatsapp.db
    .claude/
      settings.local.json        ← Claude Code dev permissions (bypassPermissions)
    CLAUDE.md                    ← Dev context for Claude Code
    AGENTS.md                    ← Dev context for Codex

  bridge-data/                   ← NOT in git; all runtime user state
    sessions.json
    codex_sessions.json
    allowed_chats.json
    codex_allowed_chats.json
    signal_allowed_chats.json
    signal_codex_allowed_chats.json
    chat_personalities.json
    settings.json
    chats/
      <chatJID>/                 ← per-chat working dir
        CLAUDE.md or AGENTS.md   ← personality context
        .claude/
          settings.local.json    ← bypassPermissions for this chat
        media/                   ← downloaded attachments
        cache.json               ← recent message cache (anti-loop)

  whatsapp-mcp-server/           ← Python MCP server (reads messages.db)
```

Path helpers in `shared.go`:
- `storeDir()` → `store/` (fixed — whatsmeow hardcodes relative path)
- `dataDir()` → `bridge-data/` (via `WHATSAPP_BRIDGE_DATA_DIR` env var)
- `configDir()` → `config/` (committed config, read-only)

---

## Key Design Decisions

**Inline handler vs. separate gateway**
All routing is inline in the Go bridge. A separate gateway would be extracted if/when a third channel (e.g. Telegram) is added.

**Session persistence via CLI `--resume`**
Rejected: Anthropic API with stored JSON history (separate billing), persistent subprocess (process management complexity). Chosen: CLI spawn-on-demand + `--resume`. Process exits after each reply; Claude Code persists session to disk. Resumed via `session_id` on next call.

**bridge-data/ outside the repo**
Runtime user state (sessions, allowed chats, per-chat dirs) lives in a sibling directory outside `whatsapp-bridge/`. This keeps the repo clean, makes backups easy, and allows the bridge directory to be replaced (git pull / reinstall) without touching user data.

**Per-chat working directory**
Claude and Codex are invoked with `cwd = bridge-data/chats/<chatJID>/`. This lets each chat have its own `CLAUDE.md`/`AGENTS.md` personality context, and any files Claude writes during a session are scoped to that chat.

**Anti-loop detection**
`shared.go` tracks recent outgoing messages per chat. If the bridge detects it is about to send a message identical (or very similar) to one it just sent, it aborts to prevent runaway loops.
