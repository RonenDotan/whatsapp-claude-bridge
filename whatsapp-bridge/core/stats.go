package core

import "sync"

type ModelUsageEntry struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CacheReadTokens int     `json:"cache_read_input_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

type UsageStats struct {
	CacheReadTokens  int                        `json:"cache_read_input_tokens"`
	CacheWriteTokens int                        `json:"cache_creation_input_tokens"`
	InputTokens      int                        `json:"input_tokens"`
	OutputTokens     int                        `json:"output_tokens"`
	TotalCostUSD     float64                    `json:"total_cost_usd"`
	DurationMs       int                        `json:"duration_ms"`
	ModelUsage       map[string]ModelUsageEntry `json:"model_usage,omitempty"`
	LastUpdated      string
}

type CodexStats struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	LastUpdated  string
}

var (
	UsageStatsMu  sync.Mutex
	UsageStatsMap = make(map[string]UsageStats)
	CodexStatsMu  sync.Mutex
	CodexStatsMap = make(map[string]CodexStats)
)

func GetUsageStats(chatID string) (UsageStats, bool) {
	UsageStatsMu.Lock()
	s, ok := UsageStatsMap[chatID]
	UsageStatsMu.Unlock()
	return s, ok
}

func GetCodexStats(chatID string) (CodexStats, bool) {
	CodexStatsMu.Lock()
	s, ok := CodexStatsMap[chatID]
	CodexStatsMu.Unlock()
	return s, ok
}
