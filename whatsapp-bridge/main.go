package main

import "log"

func main() {
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
