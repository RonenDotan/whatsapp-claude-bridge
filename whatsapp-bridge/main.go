package main

import (
	"fmt"
	"log"
	"os"

	"whatsapp-client/channels/signal"
	"whatsapp-client/channels/whatsapp"
	"whatsapp-client/core"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version" || os.Args[1] == "-v") {
		fmt.Println(Version)
		os.Exit(0)
	}

	core.BridgeVersion = Version
	settings := core.LoadSettings()
	log.Printf("Starting bridge: WhatsApp=%v Signal=%v", settings.WhatsAppEnabled, settings.SignalEnabled)

	core.InitAllowedChats()
	core.InitCodexAllowedChats()
	core.InitSignalAllowedChats()
	core.InitSignalCodexAllowedChats()
	core.InitChatPersonalities()

	if settings.WhatsAppEnabled {
		go whatsapp.Start()
	}
	if settings.SignalEnabled {
		signal.InitOwnerNumber()
		go signal.StartListener()
	}

	select {}
}
