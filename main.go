package main

import (
	"context"
	"flag"
	"log"

	tea "charm.land/bubbletea/v2"
	"github.com/raumornie/gsm-update/secretmanager"
	"github.com/raumornie/gsm-update/ui"
)

func main() {
	project := flag.String("project", "", "GCP project ID to open directly (skips the project prompt)")
	flag.StringVar(project, "p", "", "shorthand for -project")
	flag.Parse()

	client, err := secretmanager.NewClient(context.Background())
	if err != nil {
		log.Fatalf("creating secret manager client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("closing secret manager client: %v", err)
		}
	}()

	p := tea.NewProgram(ui.NewModel(client, *project))
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
