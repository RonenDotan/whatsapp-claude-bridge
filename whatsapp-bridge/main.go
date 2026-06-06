package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version" || os.Args[1] == "-v") {
		fmt.Println(Version)
		os.Exit(0)
	}

	settings := loadSettings()
	log.Printf("Starting bridge: WhatsApp=%v Signal=%v", settings.WhatsAppEnabled, settings.SignalEnabled)

	initAllowedChats()
	initCodexAllowedChats()
	initSignalAllowedChats()
	initSignalCodexAllowedChats()
	initChatPersonalities()

	if settings.WhatsAppEnabled {
		go startWhatsApp()
	}
	if settings.SignalEnabled {
		initSignalOwnerNumber()
		go startSignalListener()
	}

	select {}
}
