# gsm-update

A terminal-based interactive editor for **Google Cloud Secret Manager**. Provides a TUI (Terminal User Interface) to view and edit secrets stored in GCP's Secret Manager service.

## Overview

`gsm-update` is a Go application that lets you manage Google Cloud Secret Manager secrets directly from your terminal. It uses the [Bubbletea](https://github.com/charmbracelet/bubbletea) framework for the interactive UI and the official GCP Secret Manager Go client for API interactions.

### Key Features

- **Interactive TUI**: Navigate secrets using keyboard controls in a styled terminal interface
- **Fuzzy Filtering**: Press `/` to narrow the secret list with fuzzy matching
- **Secret Editing**: View and edit secret payload content directly in the terminal
- **Version Management**: Saving creates a new secret version and automatically disables previous versions
- **Project-Based Selection**: Enter a GCP project ID to browse all available secrets

## Prerequisites

- [Go 1.25](https://go.dev/dl/) or later
- A Google Cloud Platform (GCP) account with the **Secret Manager API** enabled
- Valid GCP credentials (see Authentication below)

### Authentication

The application uses [Application Default Credentials (ADC)](https://cloud.google.com/docs/authentication/application-default-credentials). Set up authentication by running one of the following:

```bash
# Using gcloud CLI (recommended)
gcloud auth application-default login

# Or set a service account key
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account-key.json"
```

Ensure your GCP project has the **Secret Manager API** enabled and your identity has the `roles/secretmanager.secretAccessor` IAM role.

## Installation

```bash
git clone https://github.com/raumornie/gsm-update.git
cd gsm-update
go build -o gsm-update .
```

## Usage

Run the application:

```bash
./gsm-update
```

### Workflow

The application guides you through a three-step workflow:

1. **Enter Project ID** — Type your GCP project ID and press `Enter`
2. **Select Secret** — Use `↑` / `↓` to navigate the secret list, `/` to fuzzy-filter it, then press `Enter` to select
3. **Edit Content** — Edit the secret payload inline, then save or cancel. Saving is destructive (it disables every other version of the secret), so the app asks for confirmation (`y`/`N`) before it takes effect.

### Key Bindings

| Key          | Action                                       |
|--------------|----------------------------------------------|
| `Enter`      | Confirm input / apply filter / select a secret|
| `/`          | Fuzzy-filter the secret list                  |
| `↑` / `↓`    | Navigate secret list                          |
| `PgUp`/`PgDn`| Page through the secret list                  |
| `Ctrl+S`     | Prompt to save the edited secret              |
| `y` / `N`    | Confirm / cancel the save prompt              |
| `Esc`        | Clear the filter, otherwise go back           |
| `Backspace`  | Delete previous character                     |
| `Ctrl+C`     | Quit the application                          |

## Project Structure

```
gsm-update/
├── main.go              # Application entry point and Bubbletea program setup
├── ui/
│   ├── model.go         # Application state management and event handling
│   └── views.go         # TUI view rendering with lipgloss styling
└── secretmanager/
    └── client.go        # GCP Secret Manager API wrapper
```

## Dependencies

- [`charm.land/bubbletea/v2`](https://github.com/charmbracelet/bubbletea) — Terminal UI framework
- [`charm.land/lipgloss/v2`](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [`cloud.google.com/go/secretmanager`](https://pkg.go.dev/cloud.google.com/go/secretmanager) — GCP Secret Manager client

## License

[License information if applicable]