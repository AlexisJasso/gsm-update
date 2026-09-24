package main

import (
	"context"
	"log"

	tea "charm.land/bubbletea/v2"
	"github.com/raumornie/gsm-update/secretmanager"
	"github.com/raumornie/gsm-update/ui"
)

func main() {
	client, err := secretmanager.NewClient(context.Background())
	if err != nil {
		log.Fatalf("creating secret manager client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("closing secret manager client: %v", err)
		}
	}()

	p := tea.NewProgram(ui.NewModel(client))
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
