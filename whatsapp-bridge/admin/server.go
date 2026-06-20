package admin

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// Start launches the admin HTTP server on the given port. Blocking — run in a goroutine.
func Start(port int) {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("[admin] embed error: %v", err)
		return
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/server", handleServer)
	mux.HandleFunc("/api/chats", handleChats)
	mux.HandleFunc("/api/log", handleLog)
	mux.HandleFunc("/api/settings", handleSettings)
	mux.HandleFunc("/api/cmd", handleCmd)
	mux.HandleFunc("/api/send", handleSend)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[admin] listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("[admin] server error: %v", err)
	}
}
