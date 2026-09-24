package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/raumornie/gsm-update/secretmanager"
)

// fakeService implements secretmanager.Service for tests.
type fakeService struct {
	secrets      []string
	listErr      error
	content      string
	version      string
	getErr       error
	savedPayload string
	saveCalls    int
	saveErr      error
}

func (f *fakeService) ListSecrets(projectID string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.secrets, nil
}

func (f *fakeService) GetSecretVersion(projectID, secretName string) (string, string, error) {
	if f.getErr != nil {
		return "", "", f.getErr
	}
	return f.content, f.version, nil
}

func (f *fakeService) CreateSecretVersion(projectID, secretName, payload string) error {
	f.saveCalls++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.savedPayload = payload
	return nil
}

func (f *fakeService) Close() error { return nil }

var _ secretmanager.Service = (*fakeService)(nil)

// update drives the model with a message and unwraps the returned Model.
func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(msg)
	next, ok := out.(Model)
	if !ok {
		t.Fatalf("Update(%T) returned %T, want Model", msg, out)
	}
	return next, cmd
}

// runCmd executes a command and feeds its result message back into the model.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil cmd, got nil")
	}
	next, _ := update(t, m, cmd())
	return next
}

// typeString feeds printable characters into the model one by one.
func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, ch := range s {
		m, _ = update(t, m, tea.KeyPressMsg{Text: string(ch), Code: ch})
	}
	return m
}

// drainCmd executes a command and feeds all resulting messages back into the
// model, flattening batches. The list component filters asynchronously, so
// tests that type into the filter must drain the returned commands for the
// matches to land. Commands that don't produce a message quickly are
// animations (cursor blink, spinner tick); their messages don't affect state
// assertions, so they are dropped instead of awaited.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-msgCh:
	case <-time.After(100 * time.Millisecond):
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drainCmd(t, m, c)
		}
		return m
	}
	m, _ = update(t, m, msg)
	return m
}

// typeStringDrain is typeString plus drainCmd after every keystroke, so the
// list's async filter results are applied.
func typeStringDrain(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, ch := range s {
		var cmd tea.Cmd
		m, cmd = update(t, m, tea.KeyPressMsg{Text: string(ch), Code: ch})
		m = drainCmd(t, m, cmd)
	}
	return m
}

// Key helpers mirroring how Bubbletea v2 reports keys: printable characters
// carry Text, special keys carry only Code, and modifier combos (like Ctrl+S)
// have empty Text with Mod set.
func enterKey() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func escKey() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func backspaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }
func upKey() tea.KeyPressMsg        { return tea.KeyPressMsg{Code: tea.KeyUp} }
func downKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyDown} }
func pgUpKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyPgUp} }
func pgDownKey() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyPgDown} }
func homeKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyHome} }
func endKey() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyEnd} }
func ctrlSKey() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl} }

// toSelection drives the model from project input to the secret list.
func toSelection(t *testing.T, m Model, project string) Model {
	t.Helper()
	m = typeString(t, m, project)
	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	m = runCmd(t, m, cmd) // listSecretsResult
	if m.state != StateSecretSelection {
		t.Fatalf("state = %v, want StateSecretSelection", m.state)
	}
	return m
}

// toEditor drives the model from project input to the content editor.
func toEditor(t *testing.T, m Model, project string) Model {
	t.Helper()
	m = toSelection(t, m, project)
	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	m = runCmd(t, m, cmd) // loadSecretResult
	if m.state != StateContentEdit {
		t.Fatalf("state = %v, want StateContentEdit", m.state)
	}
	return m
}

func TestNewModelDefaults(t *testing.T) {
	m := NewModel(&fakeService{})
	if m.state != StateProjectInput {
		t.Errorf("initial state = %v, want StateProjectInput", m.state)
	}
	if m.views == nil {
		t.Error("views should not be nil")
	}
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = non-nil, want nil")
	}
}

func TestProjectInputTyping(t *testing.T) {
	m := NewModel(&fakeService{})
	m = typeString(t, m, "abc")
	m, _ = update(t, m, backspaceKey())
	m = typeString(t, m, "d")
	if got := m.projectInput.Value(); got != "abd" {
		t.Errorf("project input = %q, want %q", got, "abd")
	}
}

func TestProjectInputBackspaceMultiByte(t *testing.T) {
	// Regression: byte-wise backspace used to corrupt multi-byte UTF-8
	// input, leaving an invalid tail. The textinput deletes whole runes.
	m := NewModel(&fakeService{})
	m = typeString(t, m, "hé")
	m, _ = update(t, m, backspaceKey())
	if got := m.projectInput.Value(); got != "h" {
		t.Errorf("project input = %q, want %q", got, "h")
	}
}

func TestProjectInputEnterTriggersList(t *testing.T) {
	svc := &fakeService{secrets: []string{"a", "b"}}
	m := NewModel(svc)
	m = typeString(t, m, "proj")

	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	if cmd == nil {
		t.Fatal("Enter should return a list cmd")
	}
	if !m.loading {
		t.Error("loading should be true while listing")
	}
	m = runCmd(t, m, cmd)

	if m.state != StateSecretSelection {
		t.Errorf("state = %v, want StateSecretSelection", m.state)
	}
	if m.loading {
		t.Error("loading should be false after the list result")
	}
	if got := m.list.Items(); len(got) != 2 {
		t.Errorf("list items = %v, want 2 entries", got)
	}
}

func TestProjectInputEmptyEnterIgnored(t *testing.T) {
	m := NewModel(&fakeService{})
	m, cmd := update(t, m, enterKey())
	if cmd != nil {
		t.Error("Enter with empty project ID should not return a cmd")
	}
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
}

func TestListErrorReturnsToProjectInput(t *testing.T) {
	svc := &fakeService{listErr: errors.New("no such project")}
	m := NewModel(svc)
	m = typeString(t, m, "bad-project")

	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	m = runCmd(t, m, cmd)

	if m.err == nil {
		t.Error("expected the list error to be surfaced")
	}
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
	if m.loading {
		t.Error("loading should be false after the list result")
	}
}

func TestSecretSelectionFilter(t *testing.T) {
	svc := &fakeService{secrets: []string{"db-password", "api-key", "db-backup"}}
	m := toSelection(t, NewModel(svc), "proj")

	// "/" opens the list's filter editor.
	m, cmd := update(t, m, tea.KeyPressMsg{Text: "/", Code: '/'})
	m = drainCmd(t, m, cmd)
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("FilterState = %v, want Filtering", m.list.FilterState())
	}

	// Typing narrows the visible items (the filter runs asynchronously, so
	// the commands must be drained for the matches to land).
	m = typeStringDrain(t, m, "db")
	if got := m.list.FilterValue(); got != "db" {
		t.Errorf("FilterValue = %q, want %q", got, "db")
	}
	visible := m.list.VisibleItems()
	if len(visible) != 2 {
		t.Fatalf("VisibleItems = %v, want 2 entries", visible)
	}
	names := map[string]bool{visible[0].(secretItem).Title(): true, visible[1].(secretItem).Title(): true}
	if !names["db-password"] || !names["db-backup"] {
		t.Errorf("VisibleItems = %v, want db-password and db-backup", visible)
	}
}

func TestSecretSelectionEscPeelsFilterBeforeLeaving(t *testing.T) {
	svc := &fakeService{secrets: []string{"db-password", "api-key"}}
	m := toSelection(t, NewModel(svc), "proj")

	m, cmd := update(t, m, tea.KeyPressMsg{Text: "/", Code: '/'})
	m = drainCmd(t, m, cmd)
	m = typeStringDrain(t, m, "db")
	m, cmd = update(t, m, enterKey()) // apply the filter
	m = drainCmd(t, m, cmd)
	if m.list.FilterState() != list.FilterApplied {
		t.Fatalf("FilterState = %v, want FilterApplied", m.list.FilterState())
	}

	// The first Esc only clears the applied filter; the screen stays.
	m, cmd = update(t, m, escKey())
	m = drainCmd(t, m, cmd)
	if m.state != StateSecretSelection {
		t.Errorf("state = %v, want StateSecretSelection (Esc should only clear the filter)", m.state)
	}
	if m.list.FilterState() != list.Unfiltered {
		t.Errorf("FilterState = %v, want Unfiltered after Esc", m.list.FilterState())
	}

	// The second Esc, with no filter set, leaves for the project input.
	m, _ = update(t, m, escKey())
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
}

func TestSecretSelectionNavigation(t *testing.T) {
	svc := &fakeService{secrets: []string{"a", "b", "c"}}
	m := toSelection(t, NewModel(svc), "proj")

	m, _ = update(t, m, downKey())
	if m.list.Index() != 1 {
		t.Fatalf("Index after down = %d, want 1", m.list.Index())
	}
	m, _ = update(t, m, downKey())
	m, _ = update(t, m, downKey())
	if m.list.Index() != 2 {
		t.Errorf("Index after extra down = %d, want 2 (clamped)", m.list.Index())
	}
	m, _ = update(t, m, upKey())
	m, _ = update(t, m, upKey())
	m, _ = update(t, m, upKey())
	if m.list.Index() != 0 {
		t.Errorf("Index after extra up = %d, want 0 (clamped)", m.list.Index())
	}
}

func TestSecretSelectionPageNavigation(t *testing.T) {
	svc := &fakeService{secrets: make([]string, 0, 30)}
	for i := 0; i < 30; i++ {
		svc.secrets = append(svc.secrets, fmt.Sprintf("s%02d", i))
	}
	m := toSelection(t, NewModel(svc), "proj")
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})

	perPage := m.list.Paginator.PerPage
	if perPage < 2 {
		t.Fatalf("Paginator.PerPage = %d, want at least 2 for paging tests", perPage)
	}

	m, _ = update(t, m, pgDownKey())
	if m.list.Index() != perPage {
		t.Errorf("Index after PgDown = %d, want %d (one page)", m.list.Index(), perPage)
	}
	m, _ = update(t, m, pgUpKey())
	if m.list.Index() != 0 {
		t.Errorf("Index after PgUp = %d, want 0", m.list.Index())
	}
	m, _ = update(t, m, endKey())
	if m.list.Index() != 29 {
		t.Errorf("Index after End = %d, want 29", m.list.Index())
	}
	m, _ = update(t, m, homeKey())
	if m.list.Index() != 0 {
		t.Errorf("Index after Home = %d, want 0", m.list.Index())
	}
}

func TestListViewRendersSelectedItem(t *testing.T) {
	svc := &fakeService{secrets: make([]string, 0, 30)}
	for i := 0; i < 30; i++ {
		svc.secrets = append(svc.secrets, fmt.Sprintf("s%02d", i))
	}
	m := toSelection(t, NewModel(svc), "proj")
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m.list.Select(25)

	got := m.list.View()
	if !strings.Contains(got, "s25") {
		t.Errorf("list view should show the selected item, got:\n%s", got)
	}
	// The first item is on another page and must not be rendered.
	if strings.Contains(got, "s00") {
		t.Errorf("list view should only render the current page, got:\n%s", got)
	}
}

func TestSecretSelectionEnterLoads(t *testing.T) {
	svc := &fakeService{secrets: []string{"target"}, content: "secret-value"}
	m := toSelection(t, NewModel(svc), "proj")

	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	if cmd == nil {
		t.Fatal("Enter on a match should return a load cmd")
	}
	m = runCmd(t, m, cmd)

	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit", m.state)
	}
	if m.selectedSecret != "target" {
		t.Errorf("selectedSecret = %q, want %q", m.selectedSecret, "target")
	}
	if m.editor.Value() != "secret-value" {
		t.Errorf("content = %q, want %q", m.editor.Value(), "secret-value")
	}
}

func TestSecretSelectionEnterWhileFilteringAppliesFilter(t *testing.T) {
	// While the filter is being edited, Enter applies it rather than
	// selecting an item. A filter with no matches resets to unfiltered
	// instead of loading anything.
	svc := &fakeService{secrets: []string{"other"}}
	m := toSelection(t, NewModel(svc), "proj")

	m, cmd := update(t, m, tea.KeyPressMsg{Text: "/", Code: '/'})
	m = drainCmd(t, m, cmd)
	m = typeStringDrain(t, m, "zzz")
	if len(m.list.VisibleItems()) != 0 {
		t.Fatalf("VisibleItems = %v, want none", m.list.VisibleItems())
	}

	m, cmd = update(t, m, enterKey())
	m = drainCmd(t, m, cmd)
	if m.state != StateSecretSelection {
		t.Errorf("state = %v, want StateSecretSelection (Enter must not load)", m.state)
	}
	if m.selectedSecret != "" {
		t.Errorf("selectedSecret = %q, want empty", m.selectedSecret)
	}
	if m.list.FilterState() == list.Filtering {
		t.Error("filter should no longer be editing after Enter")
	}
}

func TestSecretSelectionEscKeepsProjectID(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}}
	m := toSelection(t, NewModel(svc), "proj")

	m, _ = update(t, m, escKey())
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
	if got := m.projectInput.Value(); got != "proj" {
		t.Errorf("project input = %q, want preserved %q", got, "proj")
	}
}

func TestLoadErrorReturnsToSelection(t *testing.T) {
	svc := &fakeService{secrets: []string{"target"}, getErr: errors.New("denied")}
	m := toSelection(t, NewModel(svc), "proj")

	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	m = runCmd(t, m, cmd)

	if m.err == nil {
		t.Error("expected the load error to be surfaced")
	}
	if m.state != StateSecretSelection {
		t.Errorf("state = %v, want StateSecretSelection", m.state)
	}
}

func TestContentEditTyping(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "start"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "!")
	m, _ = update(t, m, enterKey()) // newline
	m, _ = update(t, m, backspaceKey())
	m, _ = update(t, m, backspaceKey())
	if m.editor.Value() != "start" {
		t.Errorf("content = %q, want %q", m.editor.Value(), "start")
	}
}

func TestSaveSuccess(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "-new")
	contentBefore := m.editor.Value()

	// Ctrl+S with changes prompts for confirmation instead of saving.
	m, cmd := update(t, m, ctrlSKey())
	if cmd != nil {
		t.Error("Ctrl+S should not save immediately; it should ask for confirmation")
	}
	if m.state != StateConfirmSave {
		t.Fatalf("state = %v, want StateConfirmSave", m.state)
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0 before confirmation", svc.saveCalls)
	}

	// Confirm with 'y' to actually save.
	var saveCmd tea.Cmd
	m, saveCmd = update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	if saveCmd == nil {
		t.Fatal("confirming with 'y' should return a save cmd")
	}
	if !m.saving {
		t.Error("saving should be true while the save is in flight")
	}
	if m.editor.Value() != contentBefore {
		t.Errorf("confirming must not alter the content, got %q", m.editor.Value())
	}
	m = runCmd(t, m, saveCmd)

	if svc.savedPayload != "old-new" {
		t.Errorf("saved payload = %q, want %q", svc.savedPayload, "old-new")
	}
	if m.saving {
		t.Error("saving should be false after the save result")
	}
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
	if got := m.projectInput.Value(); got != "proj" {
		t.Errorf("project input = %q, want preserved %q", got, "proj")
	}
	if m.editor.Value() != "" {
		t.Errorf("content = %q, want cleared after save", m.editor.Value())
	}
	if m.status == "" {
		t.Error("status message should be set after a successful save")
	}
}

func TestSaveFailureKeepsEditor(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old", saveErr: errors.New("quota")}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")
	contentBefore := m.editor.Value()

	m, _ = update(t, m, ctrlSKey()) // prompts for confirmation
	var cmd tea.Cmd
	m, cmd = update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	m = runCmd(t, m, cmd)

	if m.err == nil {
		t.Error("expected the save error to be surfaced")
	}
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit (stay in the editor)", m.state)
	}
	if m.editor.Value() != contentBefore {
		t.Errorf("content = %q, want preserved %q", m.editor.Value(), contentBefore)
	}
	if m.saving {
		t.Error("saving should be false after a failed save")
	}
}

func TestSaveGuardWhileSaving(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "x"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "Y") // make an edit so the save is not skipped

	m, _ = update(t, m, ctrlSKey()) // prompts for confirmation
	var first tea.Cmd
	m, first = update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	if first == nil {
		t.Fatal("confirming with 'y' should return a save cmd")
	}
	// While the save is in flight, further confirmation input must be ignored.
	m, second := update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	if second != nil {
		t.Error("a second 'y' while saving should return a nil cmd")
	}
	_ = runCmd(t, m, first)
	if svc.saveCalls != 1 {
		t.Errorf("CreateSecretVersion calls = %d, want 1", svc.saveCalls)
	}
}

func TestSaveSkippedWhenUnchanged(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "original"}
	m := toEditor(t, NewModel(svc), "proj")

	// No edits: the editor value still equals the loaded content.
	var cmd tea.Cmd
	m, cmd = update(t, m, ctrlSKey())
	if cmd != nil {
		t.Error("Ctrl+S with no changes should not return a save cmd")
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0 (save should be skipped)", svc.saveCalls)
	}
	if m.saving {
		t.Error("saving should not be set when the save is skipped")
	}
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit (stay in the editor)", m.state)
	}
	if m.editor.Value() != "original" {
		t.Errorf("content = %q, want unchanged %q", m.editor.Value(), "original")
	}
	if m.status == "" {
		t.Error("a status message should be shown when the save is skipped")
	}
}

func TestSaveConfirmPrompt(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")
	contentBefore := m.editor.Value()

	m, cmd := update(t, m, ctrlSKey())
	if cmd != nil {
		t.Error("Ctrl+S should not save immediately, it should ask for confirmation")
	}
	if m.state != StateConfirmSave {
		t.Errorf("state = %v, want StateConfirmSave", m.state)
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0 before confirmation", svc.saveCalls)
	}
	if m.editor.Value() != contentBefore {
		t.Errorf("content = %q, want preserved %q", m.editor.Value(), contentBefore)
	}
	if m.saving {
		t.Error("saving should not be set before confirmation")
	}
}

func TestSaveConfirmNoCancels(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")
	contentBefore := m.editor.Value()

	m, _ = update(t, m, ctrlSKey())
	m, cmd := update(t, m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	if cmd != nil {
		t.Error("cancelling with 'n' should not return a save cmd")
	}
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit after cancel", m.state)
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0 after cancel", svc.saveCalls)
	}
	if m.editor.Value() != contentBefore {
		t.Errorf("content = %q, want preserved %q", m.editor.Value(), contentBefore)
	}
	if m.status == "" {
		t.Error("a status message should be shown when the save is cancelled")
	}
}

func TestSaveConfirmEscCancels(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")

	m, _ = update(t, m, ctrlSKey())
	m, cmd := update(t, m, escKey())
	if cmd != nil {
		t.Error("Esc in the confirm prompt should not return a save cmd")
	}
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit after Esc", m.state)
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0 after Esc", svc.saveCalls)
	}
}

func TestSaveConfirmIgnoresOtherKeys(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")

	m, _ = update(t, m, ctrlSKey())
	m, cmd := update(t, m, tea.KeyPressMsg{Text: "a", Code: 'a'})
	if cmd != nil {
		t.Error("an unrelated key in the confirm prompt should not return a cmd")
	}
	if m.state != StateConfirmSave {
		t.Errorf("state = %v, want StateConfirmSave (stay on the prompt)", m.state)
	}
	if svc.saveCalls != 0 {
		t.Errorf("CreateSecretVersion calls = %d, want 0", svc.saveCalls)
	}
}

func TestSaveConfirmCapsYConfirms(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")

	m, _ = update(t, m, ctrlSKey())
	m, cmd := update(t, m, tea.KeyPressMsg{Text: "Y", Code: 'Y'})
	if cmd == nil {
		t.Fatal("confirming with 'Y' should return a save cmd")
	}
	m = runCmd(t, m, cmd)
	if svc.saveCalls != 1 {
		t.Errorf("CreateSecretVersion calls = %d, want 1", svc.saveCalls)
	}
}

func TestSaveFailureReturnsFromConfirm(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "old", saveErr: errors.New("denied")}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")

	m, _ = update(t, m, ctrlSKey())
	if m.state != StateConfirmSave {
		t.Fatalf("state = %v, want StateConfirmSave", m.state)
	}
	var cmd tea.Cmd
	m, cmd = update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	m = runCmd(t, m, cmd)

	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit after a failed save", m.state)
	}
	if m.err == nil {
		t.Error("expected the save error to be surfaced")
	}
}

func TestEscapeFromContentEdit(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: "x"}
	m := toEditor(t, NewModel(svc), "proj")

	m, _ = update(t, m, escKey())
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
	if got := m.projectInput.Value(); got != "proj" {
		t.Errorf("project input = %q, want preserved %q", got, "proj")
	}
	if m.editor.Value() != "" {
		t.Errorf("content = %q, want cleared", m.editor.Value())
	}
	if m.selectedSecret != "" {
		t.Errorf("selectedSecret = %q, want cleared", m.selectedSecret)
	}
}

func TestKeyReleaseIgnored(t *testing.T) {
	// Regression: matching on the generic tea.KeyMsg would also catch
	// KeyReleaseMsg and double-process every keystroke. Only KeyPressMsg
	// should be handled.
	m := NewModel(&fakeService{})
	m, _ = update(t, m, tea.KeyReleaseMsg{Text: "a", Code: 'a'})
	if got := m.projectInput.Value(); got != "" {
		t.Errorf("key release should be ignored, project input = %q", got)
	}
}

func TestStaleLoadResultDroppedAfterEsc(t *testing.T) {
	// Regression: the user selected a secret, then hit Esc before the load
	// finished. The late result must not reopen the editor.
	svc := &fakeService{secrets: []string{"s"}, content: "payload"}
	m := toSelection(t, NewModel(svc), "proj")

	m, cmd := update(t, m, enterKey()) // starts the load
	if cmd == nil {
		t.Fatal("Enter on a match should return a load cmd")
	}
	if !m.loading {
		t.Fatal("loading should be true while the load is in flight")
	}

	m, _ = update(t, m, escKey()) // cancel back to project input
	if m.state != StateProjectInput {
		t.Fatalf("state = %v, want StateProjectInput after Esc", m.state)
	}
	if m.loading {
		t.Error("loading should be false after Esc")
	}

	m, resultCmd := update(t, m, cmd()) // the stale load result arrives
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput (stale load result must be dropped)", m.state)
	}
	if resultCmd != nil {
		t.Error("a stale load result must not focus the editor")
	}
	if m.editor.Value() != "" {
		t.Errorf("editor = %q, want empty (stale payload must not be applied)", m.editor.Value())
	}
}

func TestLoadResultForPreviousSecretDropped(t *testing.T) {
	// The user loads secret "a", cancels, then selects and loads secret "b".
	// The late result for "a" must not overwrite the editor while "b" loads.
	svc := &fakeService{secrets: []string{"a", "b"}, content: "payload-a"}
	m := toSelection(t, NewModel(svc), "proj")

	m, firstCmd := update(t, m, enterKey()) // load "a"
	if firstCmd == nil {
		t.Fatal("Enter on a match should return a load cmd")
	}
	m, _ = update(t, m, escKey()) // cancel back to project input

	// Re-list and select "b" (the second entry).
	m, listCmd := update(t, m, enterKey())
	m = runCmd(t, m, listCmd)
	m, _ = update(t, m, downKey())
	m, secondCmd := update(t, m, enterKey()) // load "b"
	if secondCmd == nil {
		t.Fatal("Enter on a match should return a load cmd")
	}

	m, resultCmd := update(t, m, firstCmd()) // stale result for "a" arrives
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit (still waiting on b)", m.state)
	}
	if resultCmd != nil {
		t.Error("a stale load result must not focus the editor")
	}
	if !m.loading {
		t.Error("loading should still be true; the stale result must not clear it")
	}
	if m.editor.Value() != "" {
		t.Errorf("editor = %q, want empty (stale payload must not be applied)", m.editor.Value())
	}
}

func TestStaleListResultDroppedAfterEsc(t *testing.T) {
	// Regression: the user pressed Enter to list secrets, then Esc'd back to
	// the project input before the list arrived. The late result must not
	// force the selection state back on them.
	svc := &fakeService{secrets: []string{"a"}}
	m := NewModel(svc)
	m = typeString(t, m, "proj")

	m, cmd := update(t, m, enterKey()) // starts the list
	if cmd == nil {
		t.Fatal("Enter should return a list cmd")
	}
	m, _ = update(t, m, escKey()) // back to project input before it finishes
	if m.state != StateProjectInput {
		t.Fatalf("state = %v, want StateProjectInput after Esc", m.state)
	}

	m, _ = update(t, m, cmd()) // the stale list result arrives
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput (stale list result must be dropped)", m.state)
	}
	if m.loading {
		t.Error("loading should be false; the stale result must not clear it")
	}
}

func TestEscIgnoredWhileSaveInFlight(t *testing.T) {
	// An in-flight save cannot be un-sent, so Esc must not navigate away
	// mid-save: the user stays on the prompt until the result arrives, which
	// then routes them back to the editor (failure) or project input (success).
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")
	m, _ = update(t, m, ctrlSKey())
	m, saveCmd := update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	if saveCmd == nil {
		t.Fatal("confirming with 'y' should return a save cmd")
	}
	if !m.saving {
		t.Fatal("saving should be true while the save is in flight")
	}

	m, cmd := update(t, m, escKey()) // try to bail out mid-save
	if cmd != nil {
		t.Error("Esc while saving should not return a cmd")
	}
	if m.state != StateConfirmSave {
		t.Errorf("state = %v, want StateConfirmSave (must not navigate away mid-save)", m.state)
	}
	if !m.saving {
		t.Error("saving should still be true; Esc must not cancel an in-flight save")
	}

	m = runCmd(t, m, saveCmd) // the save completes normally
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput after a successful save", m.state)
	}
}

func TestStaleSaveResultDropped(t *testing.T) {
	// A save result whose request generation no longer matches the model's
	// (e.g. the user navigated away while it was in flight) must be dropped.
	svc := &fakeService{secrets: []string{"s"}, content: "old"}
	m := toEditor(t, NewModel(svc), "proj")

	m = typeString(t, m, "X")
	m, _ = update(t, m, ctrlSKey())
	m, saveCmd := update(t, m, tea.KeyPressMsg{Text: "y", Code: 'y'})
	if saveCmd == nil {
		t.Fatal("confirming with 'y' should return a save cmd")
	}

	// Simulate the user navigating away: a new generation starts, the save
	// flag is cleared, and the model no longer expects this result.
	m.resetEditing()
	m.saving = false

	m, _ = update(t, m, saveCmd()) // the stale save result arrives
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput (stale save result must be dropped)", m.state)
	}
	if m.status != "" {
		t.Errorf("status = %q, want empty (stale save result must not report success)", m.status)
	}
}

// driveToLoaded drives a model with the given payload to the loaded state,
// returning the model after the load result is applied.
func driveToLoaded(t *testing.T, payload string) Model {
	t.Helper()
	svc := &fakeService{secrets: []string{"s"}, content: payload, version: "projects/p/secrets/s/versions/7"}
	m := toSelection(t, NewModel(svc), "proj")
	var cmd tea.Cmd
	m, cmd = update(t, m, enterKey())
	m = runCmd(t, m, cmd)
	return m
}

func TestLoadBinarySecretBlocked(t *testing.T) {
	// A payload with invalid UTF-8 must not enter the editor; it is marked
	// binary and shows a hex dump instead.
	binary := string([]byte{0xff, 0xfe, 0x00, 0x01})
	m := driveToLoaded(t, binary)

	if m.state != StateContentEdit {
		t.Fatalf("state = %v, want StateContentEdit", m.state)
	}
	if !m.binary {
		t.Error("binary should be true for an invalid-UTF-8 payload")
	}
	if m.editor.Value() != "" {
		t.Errorf("editor = %q, want empty (binary payload must not enter the editor)", m.editor.Value())
	}
	view := m.binaryView()
	if !strings.Contains(view, "not valid UTF-8") {
		t.Errorf("binary view should explain the reason, got:\n%s", view)
	}
	if !strings.Contains(view, "versions/7") {
		t.Errorf("binary view should name the source version, got:\n%s", view)
	}
	if !strings.Contains(view, "ff fe 00 01") {
		t.Errorf("binary view should include a hex dump, got:\n%s", view)
	}
}

func TestLoadLossySecretBlocked(t *testing.T) {
	// A payload the sanitizer would alter (tab, carriage return) is valid
	// UTF-8 but cannot round-trip through the editor, so it is blocked too.
	for _, payload := range []string{"a\tb", "a\rb", "a\x07b"} {
		m := driveToLoaded(t, payload)
		if !m.binary {
			t.Errorf("payload %q: binary should be true (sanitizer would alter it)", payload)
		}
		if m.editor.Value() != "" {
			t.Errorf("payload %q: editor = %q, want empty", payload, m.editor.Value())
		}
	}
}

func TestLoadCleanSecretEditable(t *testing.T) {
	m := driveToLoaded(t, "plain text\nwith newlines\n")
	if m.binary {
		t.Error("binary should be false for a clean text payload")
	}
	if m.editor.Value() != "plain text\nwith newlines\n" {
		t.Errorf("editor = %q, want the clean payload", m.editor.Value())
	}
}

func TestBinarySecretNotSaved(t *testing.T) {
	svc := &fakeService{secrets: []string{"s"}, content: string([]byte{0xff, 0xfe}), version: "v1"}
	m := toSelection(t, NewModel(svc), "proj")
	m, cmd := update(t, m, enterKey())
	m = runCmd(t, m, cmd)
	if !m.binary {
		t.Fatal("expected a binary secret")
	}

	m, saveCmd := update(t, m, ctrlSKey())
	if saveCmd != nil {
		t.Error("Ctrl+S on a binary secret should not produce a save cmd")
	}
	if svc.saveCalls != 0 {
		t.Errorf("saveCalls = %d, want 0 (binary secrets must not be saved)", svc.saveCalls)
	}
	if m.state != StateContentEdit {
		t.Errorf("state = %v, want StateContentEdit (stay put)", m.state)
	}
	if m.status == "" {
		t.Error("a status message should explain the save was refused")
	}
}

func TestEditorContentAlwaysSavable(t *testing.T) {
	// The editor sanitizes on every input path, so anything it holds is by
	// construction valid for saving: no payload the editor can contain is
	// blocked by the load gate's check. This documents why save-time content
	// validation is unnecessary — corruption is prevented at load, and the
	// sanitizer keeps editor content round-trippable.
	for _, in := range []string{"a\tb", "a\rb", "a\x07b", string([]byte{0xff, 0xfe})} {
		ta := textarea.New()
		ta.SetValue(in)
		if _, blocked := payloadEditBlocker(ta.Value()); blocked {
			t.Errorf("editor holds %q which the load gate would block; sanitizer should prevent this", in)
		}
	}
}

func TestPayloadEditBlocker(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		blocked bool
	}{
		{"clean text", "hello world", false},
		{"newlines", "a\nb\nc", false},
		{"trailing newline", "data\n", false},
		{"unicode", "héllo wörld ✓", false},
		{"invalid utf8", string([]byte{0xff, 0xfe}), true},
		{"tab", "a\tb", true},
		{"carriage return", "a\rb", true},
		{"control char", "a\x00b", true},
		{"bell", "a\x07b", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, blocked := payloadEditBlocker(tt.payload)
			if blocked != tt.blocked {
				t.Errorf("payloadEditBlocker(%q) blocked = %v, want %v", tt.payload, blocked, tt.blocked)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("a\tb"); got != "a    b" {
		t.Errorf("sanitize tab = %q, want %q", got, "a    b")
	}
	if got := sanitize("a\rb"); got != "a\nb" {
		t.Errorf("sanitize CR = %q, want %q", got, "a\nb")
	}
	if got := sanitize("a\x00\x07b"); got != "ab" {
		t.Errorf("sanitize control chars = %q, want %q", got, "ab")
	}
	if got := sanitize("clean\ntext\n"); got != "clean\ntext\n" {
		t.Errorf("sanitize clean = %q, want unchanged", got)
	}
}

func TestBinarySecretEscClearsState(t *testing.T) {
	// Esc from a binary secret must clear the binary flag, payload, and info
	// so the next load starts clean.
	svc := &fakeService{secrets: []string{"s"}, content: string([]byte{0xff, 0xfe}), version: "v1"}
	m := toSelection(t, NewModel(svc), "proj")
	m, cmd := update(t, m, enterKey())
	m = runCmd(t, m, cmd)
	if !m.binary {
		t.Fatal("expected a binary secret")
	}

	m, _ = update(t, m, escKey())
	if m.binary || m.binaryPayload != "" || m.binaryInfo != "" {
		t.Errorf("binary state not cleared after Esc: binary=%v payload=%q info=%q", m.binary, m.binaryPayload, m.binaryInfo)
	}
	if m.state != StateProjectInput {
		t.Errorf("state = %v, want StateProjectInput", m.state)
	}
}
