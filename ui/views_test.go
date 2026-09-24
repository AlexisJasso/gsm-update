package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestProjectInputView(t *testing.T) {
	v := NewViews()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Align(lipgloss.Center)
	inputStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("69")).Padding(1)

	tests := []struct {
		name         string
		projectID    string
		wantContains []string
	}{
		{
			name:         "empty project ID",
			projectID:    "",
			wantContains: []string{"Google Secret Manager Editor", ""},
		},
		{
			name:         "with project ID",
			projectID:    "my-gcp-project",
			wantContains: []string{"Google Secret Manager Editor", "my-gcp-project"},
		},
		{
			name:         "project ID with special characters",
			projectID:    "proj-123_test",
			wantContains: []string{"Google Secret Manager Editor", "proj-123_test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.ProjectInputView(titleStyle, inputStyle, tt.projectID)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("ProjectInputView() = %q, does not contain %q", got, want)
				}
			}
		})
	}
}

func TestSecretSelectionView(t *testing.T) {
	v := NewViews()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Align(lipgloss.Center)

	tests := []struct {
		name         string
		listContent  string
		wantContains []string
	}{
		{
			name:         "list content and footer are rendered",
			listContent:  "secret-one\nsecret-two",
			wantContains: []string{"Select Secret", "secret-one", "secret-two", "/ to filter, ↑/↓ to navigate, Enter to select, Esc to go back"},
		},
		{
			name:         "empty list content still shows footer",
			listContent:  "No secrets.",
			wantContains: []string{"No secrets.", "/ to filter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.SecretSelectionView(titleStyle, tt.listContent)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("SecretSelectionView() = %q, does not contain %q", got, want)
				}
			}
		})
	}
}

func TestContentView(t *testing.T) {
	v := NewViews()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Align(lipgloss.Center)
	editorStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("69")).Padding(1)

	tests := []struct {
		name         string
		secretName   string
		content      string
		wantContains []string
	}{
		{
			name:         "single line content",
			secretName:   "my-secret",
			content:      "hello world",
			wantContains: []string{"Editing: my-secret", "hello world"},
		},
		{
			name:         "multi-line content",
			secretName:   "config",
			content:      "line1\nline2\nline3",
			wantContains: []string{"Editing: config", "line1", "line2", "line3"},
		},
		{
			name:         "empty content shows placeholder",
			secretName:   "empty-secret",
			content:      "",
			wantContains: []string{"Editing: empty-secret", "Ctrl+S to save"},
		},
		{
			name:         "content with trailing newline",
			secretName:   "trailing",
			content:      "data\n",
			wantContains: []string{"Editing: trailing", "data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.ContentView(titleStyle, editorStyle, tt.secretName, tt.content)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("ContentView() = %q, does not contain %q", got, want)
				}
			}
		})
	}
}

func TestConfirmSaveView(t *testing.T) {
	v := NewViews()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Align(lipgloss.Center)

	got := v.ConfirmSaveView(titleStyle, "my-secret")
	for _, want := range []string{
		"Confirm Save",
		"my-secret",
		"disable all other versions",
		"(y/N)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ConfirmSaveView() = %q, does not contain %q", got, want)
		}
	}
}

func TestContentViewFooter(t *testing.T) {
	v := NewViews()
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Align(lipgloss.Center)
	editorStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("69")).Padding(1)

	got := v.ContentView(titleStyle, editorStyle, "my-secret", "some content")
	if !strings.Contains(got, "Arrows to move the cursor, Home/End to jump, Ctrl+S to save, Esc to cancel") {
		t.Errorf("ContentView() missing footer hint: %q", got)
	}
}
