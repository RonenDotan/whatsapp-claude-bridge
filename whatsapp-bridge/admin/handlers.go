package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"whatsapp-client/core"
	"whatsapp-client/llms/claude"
	"whatsapp-client/llms/codex"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// GET /api/status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := ""
	if !core.StartTime.IsZero() {
		uptime = time.Since(core.StartTime).Round(time.Second).String()
	}
	writeJSON(w, map[string]any{
		"version":  core.BridgeVersion,
		"uptime":   uptime,
		"channels": core.GetAllChannelStatuses(),
	})
}

// GET /api/health
func handleHealth(w http.ResponseWriter, r *http.Request) {
	results := map[string]any{}
	for name, state := range core.GetAllChannelStatuses() {
		results[name] = map[string]any{"ok": state.Connected, "account": state.AccountID}
	}
	results["claude_cli"] = cliCheck("claude", "--version")
	results["codex_cli"] = cliCheck("codex", "--version")
	writeJSON(w, results)
}

// GET /api/server
func handleServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":        core.BridgeVersion,
		"data_dir":       core.DataDir(),
		"ips":            localIPs(),
		"lan_ip":         lanIP(),
		"ngrok_url":      core.AdminPublicURL,
		"claude_version": cliVersion("claude", "--version"),
		"codex_version":  cliVersion("codex", "--version"),
	})
}

// GET /api/chats
func handleChats(w http.ResponseWriter, r *http.Request) {
	type chatInfo struct {
		ChatID      string           `json:"chat_id"`
		Name        string           `json:"name"`
		Channel     string           `json:"channel"`
		LLM         string           `json:"llm"`
		Personality string           `json:"personality"`
		Icon        string           `json:"icon"`
		LastTS      int64            `json:"last_ts"`
		MsgCount    int              `json:"msg_count"`
		Stats       *core.UsageStats `json:"stats,omitempty"`
		CodexStats  *core.CodexStats `json:"codex_stats,omitempty"`
	}

	allowed := core.GetAllowedChats()
	summary := core.QueryChatSummary()
	list := make([]chatInfo, 0, len(allowed))
	for chatID, llm := range allowed {
		props := core.GetChatProperties(chatID)
		info := chatInfo{
			ChatID:      chatID,
			Name:        chatDisplayName(chatID),
			Channel:     chatChannel(chatID),
			LLM:         llm,
			Personality: props.Personality,
			Icon:        props.Icon,
			LastTS:      summary[chatID].LastTS,
			MsgCount:    summary[chatID].MsgCount,
		}
		if llm == "codex" {
			if s, ok := core.GetCodexStats(chatID); ok {
				info.CodexStats = &s
			}
		} else {
			if s, ok := core.GetUsageStats(chatID); ok {
				info.Stats = &s
			}
		}
		list = append(list, info)
	}
	writeJSON(w, list)
}

// GET /api/log?chat=&limit=&before=
func handleLog(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("chat")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)

	entries, err := core.QueryLog(chatID, limit, before)
	if err != nil {
		errJSON(w, err.Error(), 500)
		return
	}
	if entries == nil {
		entries = []core.LogEntry{}
	}
	writeJSON(w, entries)
}

// GET /api/settings  — return current settings
// POST /api/settings — save new settings
func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, core.LoadSettings())
		return
	}
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", 405)
		return
	}
	var s core.Settings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		errJSON(w, "invalid JSON", 400)
		return
	}
	if s.AdminPort == 0 {
		s.AdminPort = 8081
	}
	if err := core.SaveSettings(s); err != nil {
		errJSON(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// POST /api/cmd  body: {"chat_id":"...","command":"!clear-session"}
func handleCmd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", 405)
		return
	}
	var body struct {
		ChatID  string `json:"chat_id"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChatID == "" || body.Command == "" {
		errJSON(w, "chat_id and command are required", 400)
		return
	}

	props := core.GetChatProperties(body.ChatID)
	result := core.HandleBridgeCommand(body.ChatID, body.Command, true, props)

	if result.ClearSession {
		core.DeleteSession(body.ChatID)
		core.DeleteCodexSession(body.ChatID)
		core.ClearInputHistory(body.ChatID)
	}
	if result.InvalidateProps {
		core.InvalidateChatProperties(body.ChatID)
	}
	if result.RemoveFromAllowed {
		core.RemoveChatFromAllowed(body.ChatID)
		core.SaveAllowedChats()
	}

	writeJSON(w, map[string]any{"reply": result.Reply})
}

// POST /api/send  body: {"chat_id":"...","text":"..."}
// Runs text through the LLM for the given chat and returns the reply.
// Does NOT route through WhatsApp/Signal — admin-only session.
func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", 405)
		return
	}
	var body struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChatID == "" || body.Text == "" {
		errJSON(w, "chat_id and text are required", 400)
		return
	}

	core.AppendLog(body.ChatID, "in", body.Text, false, 0)

	llmType := core.GetChatLLM(body.ChatID)
	var reply string

	switch llmType {
	case "codex":
		codex.HandleWithCodex(body.ChatID, body.Text, func(r string) {
			reply = r
		})
	default:
		claude.HandleWithClaude(body.ChatID, body.Text, func(r string) {
			reply = r
		})
	}

	if reply != "" {
		tokens := 0
		if s, ok := core.GetUsageStats(body.ChatID); ok {
			tokens = s.OutputTokens
		}
		core.AppendLog(body.ChatID, "out", reply, false, tokens)
	}

	writeJSON(w, map[string]any{"reply": reply})
}

// GET /api/permissions          — returns {chat_id: ChatPermission, ...} for all chats
// POST /api/permissions         — body: {chat_id, level, custom_allow?} — applies + clears session
func handlePermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		chats := core.GetAllowedChats()
		out := make(map[string]core.ChatPermission, len(chats))
		for id := range chats {
			out[id] = core.GetChatPermission(id)
		}
		writeJSON(w, out)

	case http.MethodPost:
		var body struct {
			ChatID      string   `json:"chat_id"`
			Level       string   `json:"level"`
			CustomAllow []string `json:"custom_allow"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChatID == "" || body.Level == "" {
			errJSON(w, "chat_id and level are required", 400)
			return
		}
		p := core.ChatPermission{
			Level:       core.PermLevel(body.Level),
			CustomAllow: body.CustomAllow,
		}
		llmType := core.GetChatLLM(body.ChatID)
		if err := core.ApplyPermission(body.ChatID, llmType, p); err != nil {
			errJSON(w, err.Error(), 500)
			return
		}
		core.SetChatPermission(body.ChatID, p)
		// clear sessions so next message uses new config
		core.DeleteSession(body.ChatID)
		core.DeleteCodexSession(body.ChatID)
		writeJSON(w, map[string]any{"ok": true, "level": body.Level})

	default:
		errJSON(w, "method not allowed", 405)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func chatChannel(chatID string) string {
	if strings.Contains(chatID, "@") {
		return "whatsapp"
	}
	return "signal"
}

func chatDisplayName(chatID string) string {
	if name := core.GetChatName(chatID); name != "" {
		return name
	}
	parts := strings.SplitN(chatID, "@", 2)
	num := parts[0]
	if len(parts) < 2 {
		return chatID
	}
	switch parts[1] {
	case "g.us":
		if len(num) > 6 {
			return "···" + num[len(num)-4:]
		}
		return num
	case "s.whatsapp.net":
		return "+" + num
	}
	return num
}

func cliCheck(bin string, args ...string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "version": strings.TrimSpace(string(out))}
}

func cliVersion(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func localIPs() []string {
	var ips []string
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ips = append(ips, ip.String())
	}
	return ips
}

// lanIP returns the best IPv4 LAN address for port-forwarding rules.
// Prefers 192.168.x.x (home routers) over 10.x.x.x over 172.x.x.x (WSL/VPN).
func lanIP() string {
	addrs, _ := net.InterfaceAddrs()
	var best [3]string // [0]=192.168, [1]=10., [2]=172.
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		s := ip4.String()
		switch {
		case strings.HasPrefix(s, "192.168.") && best[0] == "":
			best[0] = s
		case strings.HasPrefix(s, "10.") && best[1] == "":
			best[1] = s
		case strings.HasPrefix(s, "172.") && best[2] == "":
			best[2] = s
		}
	}
	for _, v := range best {
		if v != "" {
			return v
		}
	}
	return ""
}
