package ui

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raumornie/gsm-update/secretmanager"
)

// Model represents the application state.
type Model struct {
	// service provides access to Google Secret Manager. It is an interface so
	// tests can substitute a fake implementation.
	service secretmanager.Service

	// projectInput is the bubbles textinput for the GCP project ID. It gives
	// us rune-aware editing (no byte-wise backspace corruption), cursor
	// movement, and paste for free.
	projectInput textinput.Model

	// list is the bubbles list used for secret selection. It provides
	// fuzzy filtering ("/" to open), paging, and a status bar, replacing the
	// old hand-rolled filter/cursor/viewport trio.
	list list.Model

	// Secret editing.
	selectedSecret string
	// editor is a full multi-line text editor (bubbles textarea) with a
	// movable cursor, replacing the old append-only buffer.
	editor textarea.Model
	// loadedContent is the secret's payload as loaded into the editor (after
	// sanitizing). Comparing it against the editor's current value lets us
	// skip redundant saves when the user has not changed anything.
	loadedContent string
	// binary marks a secret whose payload cannot be edited as text: it is not
	// valid UTF-8, or the editor's sanitizer would alter it on load. Binary
	// secrets render a hex dump and refuse to save so they are never
	// corrupted by a round-trip through the editor.
	binary bool
	// binaryPayload holds the raw payload of a binary secret for the hex dump.
	// It is kept out of the editor so the sanitizer cannot alter it.
	binaryPayload string
	// binaryInfo carries the reason and source version for a binary payload,
	// for the read-only view.
	binaryInfo string

	// Transient UI state.
	state   AppState
	err     error
	status  string
	loading bool
	saving  bool
	// req is the request generation of the in-flight command. It increments
	// every time a new command starts or the user navigates away, so a result
	// arriving for an older generation is stale and must be dropped.
	req int

	views *Views
}

// AppState represents the different states of our application
type AppState int

const (
	StateProjectInput AppState = iota
	StateSecretSelection
	StateContentEdit
	StateConfirmSave
)

var (
	// Styles for our UI elements
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Align(lipgloss.Center)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("69")).
			Padding(1)

	editorStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("69")).
			Padding(1)
)

// Result messages produced by the model's commands. Keeping them as distinct
// types makes the Update loop easy to drive from tests. Each carries the
// request generation that produced it so stale results (from a request the
// user has since navigated away from) can be dropped.
type (
	// listSecretsResult carries the outcome of a listSecretsCmd.
	listSecretsResult struct {
		req     int
		secrets []string
		err     error
	}

	// loadSecretResult carries the outcome of a loadSecretCmd. version is the
	// resource name of the version the content came from.
	loadSecretResult struct {
		req     int
		content string
		version string
		err     error
	}

	// saveSecretResult carries the outcome of a saveSecretCmd.
	saveSecretResult struct {
		req int
		err error
	}
)

const (
	// listChromeH is the number of terminal rows the secret-selection view uses
	// outside the list component (title, spacing, footer, status/error). The
	// list height is bounded by the window height minus this so the whole
	// view fits.
	listChromeH = 8
)

// secretItem adapts a secret name to the list.DefaultItem interface so the
// default delegate can render it and the fuzzy filter can match on it.
type secretItem string

func (s secretItem) FilterValue() string { return string(s) }
func (s secretItem) Title() string       { return string(s) }
func (s secretItem) Description() string { return "" }

// NewModel creates the initial model. service must not be nil.
func NewModel(service secretmanager.Service) Model {
	projectInput := textinput.New()
	projectInput.Prompt = ""
	projectInput.Placeholder = "my-gcp-project"
	projectInput.CharLimit = 63 // GCP project IDs are at most 30 characters.
	projectInput.SetWidth(40)
	projectInput.Focus()

	// Single-line items with no description and no gap between rows.
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	secretList := list.New(nil, delegate, 80, 10)
	// We render our own title and footer, so hide the list's chrome except
	// the status bar ("N secrets") and the paginator.
	secretList.SetShowTitle(false)
	secretList.SetShowHelp(false)
	secretList.SetStatusBarItemName("secret", "secrets")
	// The list's default quit bindings ("v", Ctrl+C) must not exit the app;
	// quitting stays with bubbletea's Ctrl+C signal handling.
	secretList.DisableQuitKeybindings()

	return Model{
		service:      service,
		state:        StateProjectInput,
		views:        NewViews(),
		editor:       textarea.New(),
		projectInput: projectInput,
		list:         secretList,
	}
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates state
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch m.state {
		case StateProjectInput:
			return m.updateProjectInput(msg)
		case StateSecretSelection:
			return m.updateSecretSelection(msg)
		case StateContentEdit:
			return m.updateContentEdit(msg)
		case StateConfirmSave:
			return m.updateConfirmSave(msg)
		}
	case tea.WindowSizeMsg:
		// Fit the editor to the terminal: the border and padding take four
		// columns, and the title/footer take roughly eight rows.
		if msg.Width > 4 {
			m.editor.SetWidth(msg.Width - 4)
		}
		if msg.Height > 8 {
			m.editor.SetHeight(msg.Height - 8)
		}
		m.list.SetSize(msg.Width, max(1, msg.Height-listChromeH))
		return m, nil
	case listSecretsResult:
		if !m.loading || msg.req != m.req {
			// Stale result: the user navigated away (or re-listed) while the
			// command was in flight, so applying it would yank them into a
			// state they left. Drop it.
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			// Listing failed (e.g. bad project ID): go back to the project
			// input so the user can correct it.
			m.err = msg.err
			m.state = StateProjectInput
			return m, nil
		}
		m.err = nil
		m.list.ResetFilter()
		m.list.ResetSelected()
		m.list.SetItems(secretItems(msg.secrets))
		m.state = StateSecretSelection
		return m, nil
	case loadSecretResult:
		if !m.loading || m.state != StateContentEdit || msg.req != m.req {
			// Stale result (e.g. the user cancelled with Esc while the load
			// was in flight, or selected a different secret): drop it rather
			// than reopening the editor with an old payload.
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			// Loading failed: go back to the secret list so the user can
			// pick another secret.
			m.err = msg.err
			m.state = StateSecretSelection
			return m, nil
		}
		m.err = nil
		// Gate the payload before it enters the editor: if it is not valid
		// UTF-8, or the editor's sanitizer would alter it, editing it as text
		// would corrupt it. Mark it binary and show a read-only hex dump
		// instead of the editor.
		if reason, lossy := payloadEditBlocker(msg.content); lossy {
			m.binary = true
			m.binaryInfo = fmt.Sprintf("%s\n\nLoaded from %s.", reason, msg.version)
			m.binaryPayload = msg.content
			m.editor.SetValue("")
			m.loadedContent = ""
			return m, nil
		}
		m.binary = false
		m.binaryInfo = ""
		m.binaryPayload = ""
		m.editor.SetValue(msg.content)
		m.loadedContent = msg.content
		return m, m.editor.Focus()
	case saveSecretResult:
		if !m.saving || msg.req != m.req {
			// Stale result from a save that is no longer in flight.
			return m, nil
		}
		m.saving = false
		// The save was confirmed from the confirm prompt; a failure must
		// return the user to the editor, not leave them on the prompt.
		if m.state == StateConfirmSave {
			m.state = StateContentEdit
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.status = fmt.Sprintf("Saved %s as a new version; all other versions were disabled.", m.selectedSecret)
		cmd := m.resetEditing()
		return m, cmd
	case error:
		m.err = msg
		return m, nil
	}

	// Route anything else to the list while it is active: its async filter
	// command delivers FilterMatchesMsg outside the key-handling path, and
	// those messages must reach the list for filtering to work.
	if m.state == StateSecretSelection {
		newList, cmd := m.list.Update(msg)
		m.list = newList
		return m, cmd
	}

	return m, nil
}

// secretItems wraps secret names as list items.
func secretItems(names []string) []list.Item {
	items := make([]list.Item, 0, len(names))
	for _, name := range names {
		items = append(items, secretItem(name))
	}
	return items
}

// binaryView renders the read-only view of a binary (non-text-editable)
// secret: the reason it is blocked plus a hex dump of the payload.
func (m Model) binaryView() string {
	return m.binaryInfo + "\n\n" + hex.Dump([]byte(m.binaryPayload))
}

// View renders the UI
func (m Model) View() tea.View {
	var s strings.Builder

	switch m.state {
	case StateProjectInput:
		s.WriteString(m.views.ProjectInputView(titleStyle, inputStyle, m.projectInput.View()))
	case StateSecretSelection:
		s.WriteString(m.views.SecretSelectionView(titleStyle, m.list.View()))
	case StateContentEdit:
		if m.binary {
			s.WriteString(m.views.ContentView(titleStyle, editorStyle, m.selectedSecret, m.binaryView()))
		} else {
			s.WriteString(m.views.ContentView(titleStyle, editorStyle, m.selectedSecret, m.editor.View()))
		}
	case StateConfirmSave:
		s.WriteString(m.views.ConfirmSaveView(titleStyle, m.selectedSecret))
	}

	if m.loading {
		s.WriteString("\nLoading...")
	}

	if m.status != "" {
		s.WriteString("\n\n")
		s.WriteString(m.status)
	}

	if m.err != nil {
		s.WriteString(fmt.Sprintf("\n\nError: %v", m.err))
	}

	view := tea.NewView(s.String())
	// Run in the alternate screen so secret payloads never persist in the
	// terminal's scrollback buffer after the program exits.
	view.AltScreen = true
	return view
}

// listSecretsCmd lists the secrets in the current project. The result is
// tagged with the current request generation so a stale result is dropped.
func (m Model) listSecretsCmd() tea.Cmd {
	req := m.req
	return func() tea.Msg {
		secrets, err := m.service.ListSecrets(m.projectInput.Value())
		return listSecretsResult{req: req, secrets: secrets, err: err}
	}
}

// loadSecretCmd loads the most recently created enabled version of the
// selected secret. The result is tagged with the current request generation
// so a stale result is dropped.
func (m Model) loadSecretCmd() tea.Cmd {
	req := m.req
	return func() tea.Msg {
		content, version, err := m.service.GetSecretVersion(m.projectInput.Value(), m.selectedSecret)
		return loadSecretResult{req: req, content: content, version: version, err: err}
	}
}

// sanitize mirrors the bubbles textarea's rune sanitizer: it strips control
// characters (except tab and newline), expands tabs to four spaces, and
// normalizes carriage returns to newlines. Comparing sanitize(s) with s tells
// us whether the editor would alter s on load or on save.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\r':
			b.WriteByte('\n')
		case r == '\n':
			b.WriteByte('\n')
		case unicode.IsControl(r):
			// Other control characters are dropped by the editor's sanitizer.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// payloadEditBlocker reports whether a secret payload cannot be safely edited
// as text, and a human-readable reason. A payload is blocked when it is not
// valid UTF-8 (a binary payload) or when the editor's sanitizer would alter
// it on the way in or out (tabs, carriage returns, or control characters),
// because a round-trip through the editor would then corrupt it.
func payloadEditBlocker(payload string) (reason string, blocked bool) {
	if !utf8.ValidString(payload) {
		return "This secret is not valid UTF-8 (it looks like binary data).", true
	}
	if sanitize(payload) != payload {
		return "This secret contains tabs, carriage returns, or control characters that the editor cannot preserve.", true
	}
	return "", false
}

// saveSecretCmd saves the edited content as a new secret version and disables
// all other versions of the secret. The result is tagged with the current
// request generation so a stale result is dropped.
func (m Model) saveSecretCmd() tea.Cmd {
	req := m.req
	return func() tea.Msg {
		err := m.service.CreateSecretVersion(m.projectInput.Value(), m.selectedSecret, m.editor.Value())
		return saveSecretResult{req: req, err: err}
	}
}

// resetEditing returns to the project input state and clears the working
// secret, keeping the project ID so the user can immediately pick another
// secret. It returns the project input's focus command so the cursor resumes
// blinking.
func (m *Model) resetEditing() tea.Cmd {
	m.state = StateProjectInput
	m.selectedSecret = ""
	m.editor.SetValue("")
	m.loadedContent = ""
	m.binary = false
	m.binaryInfo = ""
	m.binaryPayload = ""
	m.editor.Blur()
	m.loading = false
	// Invalidate any in-flight command so its result is dropped as stale.
	m.req++
	return m.projectInput.Focus()
}

// updateProjectInput handles key input while entering the project ID. Enter
// starts the secret listing; everything else is delegated to the textinput,
// which provides rune-aware editing, cursor movement, and paste.
func (m Model) updateProjectInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Code == tea.KeyEnter {
		if m.projectInput.Value() == "" {
			return m, nil
		}
		m.err = nil
		m.loading = true
		m.req++
		m.state = StateSecretSelection
		return m, m.listSecretsCmd()
	}

	newInput, cmd := m.projectInput.Update(key)
	m.projectInput = newInput
	return m, cmd
}

// updateSecretSelection handles key input while choosing a secret. The list
// component does the heavy lifting: "/" opens fuzzy filtering, arrows and
// PgUp/PgDn navigate, Home/End jump to the ends. Enter selects an item (but
// while the filter is being edited, Enter applies the filter instead). Esc
// peels off an active filter first; only with no filter set does it leave
// the selection screen.
func (m Model) updateSecretSelection(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEnter && m.list.FilterState() != list.Filtering:
		item := m.list.SelectedItem()
		if item == nil {
			// No items, or a filter with no matches: nothing to select.
			return m, nil
		}
		m.err = nil
		m.selectedSecret = string(item.(secretItem))
		m.loading = true
		m.req++
		m.state = StateContentEdit
		return m, m.loadSecretCmd()
	case key.Code == tea.KeyEscape && m.list.FilterState() == list.Unfiltered:
		// No filter to peel off: leave the selection screen.
		m.loading = false
		// Invalidate any in-flight list command so its result is dropped.
		m.req++
		m.state = StateProjectInput
		return m, nil
	}

	newList, cmd := m.list.Update(key)
	m.list = newList
	return m, cmd
}

// updateContentEdit handles key input while editing the secret payload.
// Ctrl+S saves and Esc cancels; every other key is delegated to the textarea
// component, which provides a real editor with a movable cursor, in-line
// editing, word/line navigation and scrolling.
func (m Model) updateContentEdit(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Mod.Contains(tea.ModCtrl) && key.Code == 's':
		// Save the secret as a new version. Bubbletea v2 reports Ctrl+S with
		// an empty Text and ModCtrl set, so the check must use Mod, not Text.
		if m.saving || m.loading {
			return m, nil
		}
		if m.binary {
			// Binary payloads are never editable, so they are never saved.
			m.status = "Binary secrets are read-only and cannot be saved."
			return m, nil
		}
		// Note: there is no save-time content check here. The editor's
		// sanitizer runs on every input path, so the editor can never hold
		// content that the load gate would have rejected. The corruption risk
		// is fully handled at load time by payloadEditBlocker.
		if m.editor.Value() == m.loadedContent {
			// Nothing changed since the secret was loaded: skip the save so we
			// don't create a redundant version and disable the others.
			m.status = "No changes to save."
			return m, nil
		}
		// Saving is destructive (it disables every other version), so ask for
		// confirmation before actually saving.
		m.err = nil
		m.status = ""
		m.state = StateConfirmSave
		return m, nil
	case key.Code == tea.KeyEscape:
		cmd := m.resetEditing()
		return m, cmd
	}

	newEditor, cmd := m.editor.Update(key)
	m.editor = newEditor
	return m, cmd
}

// updateConfirmSave handles the save-confirmation prompt. 'y' confirms and
// starts the save; 'n' or Esc cancels and returns to the editor. Any other
// key is ignored so a stray keystroke can never trigger a destructive save.
func (m Model) updateConfirmSave(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.saving {
		// A save is already in flight and cannot be un-sent; ignore all
		// input (including Esc) until its result arrives, so the user is
		// never left stranded on the prompt or navigated away mid-save.
		return m, nil
	}
	switch {
	case key.Text == "y" || key.Text == "Y" || key.Code == 'y' || key.Code == 'Y':
		m.saving = true
		m.err = nil
		m.req++
		return m, m.saveSecretCmd()
	case key.Code == tea.KeyEscape || key.Text == "n" || key.Text == "N" || key.Code == 'n' || key.Code == 'N':
		m.state = StateContentEdit
		m.status = "Save cancelled."
		return m, nil
	}

	return m, nil
}
