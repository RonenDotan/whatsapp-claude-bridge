package core

import (
	"strings"
	"sync"
	"unicode"
)

var (
	inputHistoryMu sync.Mutex
	inputHistory   = make(map[string][]string)
)

func ClearInputHistory(chatID string) {
	inputHistoryMu.Lock()
	delete(inputHistory, chatID)
	inputHistoryMu.Unlock()
}

func AddToInputHistory(chatJID, msg string) {
	inputHistoryMu.Lock()
	defer inputHistoryMu.Unlock()
	h := inputHistory[chatJID]
	h = append(h, msg)
	if len(h) > 5 {
		h = h[len(h)-5:]
	}
	inputHistory[chatJID] = h
}

func normalizeForSimilarity(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

func isSimilar(a, b string) bool {
	wordsA := normalizeForSimilarity(a)
	wordsB := normalizeForSimilarity(b)
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return true
	}
	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return true
	}
	return float64(intersection)/float64(union) >= 0.80
}

func IsLooping(chatJID, newMsg string) bool {
	inputHistoryMu.Lock()
	defer inputHistoryMu.Unlock()
	history := inputHistory[chatJID]
	similar := 0
	for _, prev := range history {
		if isSimilar(prev, newMsg) {
			similar++
		}
	}
	return similar >= 2
}
