package core

import (
	"database/sql"
	"log"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// LogEntry is one row in the conversation log.
type LogEntry struct {
	ID            int64     `json:"id"`
	Timestamp     time.Time `json:"ts"`
	ChatID        string    `json:"chat_id"`
	Direction     string    `json:"direction"`
	Text          string    `json:"text"`
	HasAttachment bool      `json:"has_attachment"`
	TokensUsed    int       `json:"tokens_used"`
}

var convDB *sql.DB

// InitConvLog opens (or creates) bridge-data/conversations.db.
// Call once at startup before any Append calls.
func InitConvLog() {
	path := filepath.Join(DataDir(), "conversations.db")
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		log.Printf("[convlog] failed to open %s: %v", path, err)
		return
	}
	db.SetMaxOpenConns(1)
	if err := initConvSchema(db); err != nil {
		log.Printf("[convlog] schema init failed: %v", err)
		db.Close()
		return
	}
	convDB = db
	log.Printf("[convlog] opened %s", path)
}

func initConvSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS log (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			ts             INTEGER NOT NULL,
			chat_id        TEXT    NOT NULL,
			direction      TEXT    NOT NULL,
			text           TEXT    NOT NULL DEFAULT '',
			has_attachment INTEGER NOT NULL DEFAULT 0,
			tokens_used    INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS log_chat ON log(chat_id, ts DESC);
		CREATE INDEX IF NOT EXISTS log_ts   ON log(ts DESC);
	`)
	return err
}

// AppendLog writes one entry to the conversation log.
// direction must be "in" or "out". Safe to call if InitConvLog failed (no-op).
func AppendLog(chatID, direction, text string, hasAttachment bool, tokensUsed int) {
	if convDB == nil {
		return
	}
	att := 0
	if hasAttachment {
		att = 1
	}
	_, err := convDB.Exec(
		`INSERT INTO log(ts, chat_id, direction, text, has_attachment, tokens_used) VALUES(?,?,?,?,?,?)`,
		time.Now().UnixMilli(), chatID, direction, text, att, tokensUsed,
	)
	if err != nil {
		log.Printf("[convlog] append error: %v", err)
	}
}

// ChatSummary holds per-chat stats derived from the log.
type ChatSummary struct {
	LastTS   int64
	MsgCount int
}

// QueryChatSummary returns last-message timestamp and total message count per chat ID.
func QueryChatSummary() map[string]ChatSummary {
	out := map[string]ChatSummary{}
	if convDB == nil {
		return out
	}
	rows, err := convDB.Query(`SELECT chat_id, MAX(ts), COUNT(*) FROM log GROUP BY chat_id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var chatID string
		var s ChatSummary
		if rows.Scan(&chatID, &s.LastTS, &s.MsgCount) == nil {
			out[chatID] = s
		}
	}
	return out
}

// QueryLog returns up to limit entries for chatID (all chats if chatID==""),
// with ts < beforeMS (pass 0 for newest-first from now).
func QueryLog(chatID string, limit int, beforeMS int64) ([]LogEntry, error) {
	if convDB == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeMS <= 0 {
		beforeMS = time.Now().UnixMilli() + 1
	}

	var rows *sql.Rows
	var err error
	if chatID == "" {
		rows, err = convDB.Query(
			`SELECT id, ts, chat_id, direction, text, has_attachment, tokens_used
			   FROM log WHERE ts < ? ORDER BY ts DESC LIMIT ?`,
			beforeMS, limit,
		)
	} else {
		rows, err = convDB.Query(
			`SELECT id, ts, chat_id, direction, text, has_attachment, tokens_used
			   FROM log WHERE chat_id = ? AND ts < ? ORDER BY ts DESC LIMIT ?`,
			chatID, beforeMS, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var tsMS int64
		var att int
		if err := rows.Scan(&e.ID, &tsMS, &e.ChatID, &e.Direction, &e.Text, &att, &e.TokensUsed); err != nil {
			return nil, err
		}
		e.Timestamp = time.UnixMilli(tsMS)
		e.HasAttachment = att != 0
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
