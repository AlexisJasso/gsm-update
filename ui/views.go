package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Views renders the application screens. It owns the lipgloss styles so
// they are defined once, here, instead of being threaded through every
// view call.
type Views struct {
	// title styles the screen headings.
	title lipgloss.Style
	// input frames the project-ID text input.
	input lipgloss.Style
	// editor frames the secret content editor.
	editor lipgloss.Style
}

func NewViews() *Views {
	// The input and editor frames share one look; lipgloss styles are
	// values, so both fields can hold the same one and still diverge later.
	frame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("69")).
		Padding(1)
	return &Views{
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Align(lipgloss.Center),
		input:  frame,
		editor: frame,
	}
}

// ProjectInputView renders the project-ID prompt. input is the pre-rendered
// view of the text input component (including its cursor).
func (v *Views) ProjectInputView(input string) string {
	return fmt.Sprintf(
		"%s\n\nEnter GCP Project ID:\n%s\n",
		v.title.Render("Google Secret Manager Editor"),
		v.input.Render(input),
	)
}

// SecretSelectionView renders the secret-selection screen. listContent is the
// pre-rendered view of the list component, which already includes its own
// filter input, status bar, and pagination.
func (v *Views) SecretSelectionView(listContent string) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("%s\n\n", v.title.Render("Select Secret")))
	s.WriteString(listContent)
	s.WriteString("\n")
	s.WriteString("/ to filter, ↑/↓ to navigate, Enter to select, Esc to go back, Ctrl+C to quit")
	return s.String()
}

// ConfirmSaveView renders the confirmation prompt shown before a destructive
// save (saving a new version disables every other version of the secret).
// The 'N' default means only an explicit 'y' confirms.
func (v *Views) ConfirmSaveView(secretName string) string {
	return fmt.Sprintf(
		"%s\n\nSave %s as a new version and disable all other versions? (y/N)\n\ny to save, n or Esc to cancel",
		v.title.Render("Confirm Save"),
		secretName,
	)
}

// ContentView renders the editing screen. editor is the pre-rendered view of
// the text editor component (including its cursor), so the view function no
// longer needs to format the payload itself.
func (v *Views) ContentView(secretName, editor string) string {
	return fmt.Sprintf(
		"%s\n\n%s\n\nArrows to move the cursor, Home/End to jump, Ctrl+S to save, Esc to cancel, Ctrl+C to quit",
		v.title.Render(fmt.Sprintf("Editing: %s", secretName)),
		v.editor.Render(editor),
	)
}
