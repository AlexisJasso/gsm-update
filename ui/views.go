package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Views contains the UI view functions
type Views struct{}

func NewViews() *Views {
	return &Views{}
}

// ProjectInputView renders the project-ID prompt. input is the pre-rendered
// view of the text input component (including its cursor).
func (v *Views) ProjectInputView(titleStyle, inputStyle lipgloss.Style, input string) string {
	return fmt.Sprintf(
		"%s\n\nEnter GCP Project ID:\n%s\n",
		titleStyle.Render("Google Secret Manager Editor"),
		inputStyle.Render(input),
	)
}

// SecretSelectionView renders the secret-selection screen. listContent is the
// pre-rendered view of the list component, which already includes its own
// filter input, status bar, and pagination.
func (v *Views) SecretSelectionView(titleStyle lipgloss.Style, listContent string) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("%s\n\n", titleStyle.Render("Select Secret")))
	s.WriteString(listContent)
	s.WriteString("\n")
	s.WriteString("/ to filter, ↑/↓ to navigate, Enter to select, Esc to go back, Ctrl+C to quit")
	return s.String()
}

// ConfirmSaveView renders the confirmation prompt shown before a destructive
// save (saving a new version disables every other version of the secret).
// The 'N' default means only an explicit 'y' confirms.
func (v *Views) ConfirmSaveView(titleStyle lipgloss.Style, secretName string) string {
	return fmt.Sprintf(
		"%s\n\nSave %s as a new version and disable all other versions? (y/N)\n\ny to save, n or Esc to cancel",
		titleStyle.Render("Confirm Save"),
		secretName,
	)
}

// ContentView renders the editing screen. editor is the pre-rendered view of
// the text editor component (including its cursor), so the view function no
// longer needs to format the payload itself.
func (v *Views) ContentView(titleStyle, editorStyle lipgloss.Style, secretName, editor string) string {
	return fmt.Sprintf(
		"%s\n\n%s\n\nArrows to move the cursor, Home/End to jump, Ctrl+S to save, Esc to cancel, Ctrl+C to quit",
		titleStyle.Render(fmt.Sprintf("Editing: %s", secretName)),
		editorStyle.Render(editor),
	)
}
