package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func formKey(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sendForm(t *testing.T, m sectionFormModel, msg tea.Msg) (sectionFormModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(sectionFormModel)
	require.True(t, ok, "Update returned %T", next)
	return got, cmd
}

func formFieldIndex(t *testing.T, m sectionFormModel, path string) int {
	t.Helper()
	for i, f := range m.fields {
		if f.path == path {
			return i
		}
	}
	t.Fatalf("field %q not found", path)
	return -1
}

func moveFormCursor(t *testing.T, m sectionFormModel, to int) sectionFormModel {
	t.Helper()
	for m.cursor < to {
		m, _ = sendForm(t, m, formKey("down"))
	}
	for m.cursor > to {
		m, _ = sendForm(t, m, formKey("up"))
	}
	return m
}

func TestNewSectionFormModelLoadsFieldsAndDefaults(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TLS = &config.TLSConfig{Cert: "/tmp/cert.pem"}
	cfg.Server.NoTLS = true

	m := newSectionFormModel("Server Settings", "server", &cfg, "unused.yaml")
	assert.Equal(t, "Server Settings", m.title)
	assert.Equal(t, len(config.SectionFields("server")), len(m.fields))
	assert.Equal(t, 0, m.cursor)

	port := m.fields[formFieldIndex(t, m, "server.port")]
	assert.False(t, port.isBool)
	assert.Equal(t, "16677", port.input.Value())
	assert.True(t, port.input.Focused(), "first text field starts focused")
	assert.Equal(t, 48, port.input.Width)

	noTLS := m.fields[formFieldIndex(t, m, "server.no_tls")]
	assert.True(t, noTLS.isBool)
	assert.True(t, noTLS.value)
	assert.False(t, m.fields[formFieldIndex(t, m, "server.external_access")].value)

	cert := m.fields[formFieldIndex(t, m, "server.tls.cert")]
	assert.Equal(t, "/tmp/cert.pem", cert.input.Value())
	assert.Equal(t, "/path/to/cert.pem", cert.input.Placeholder)
	assert.False(t, cert.input.Focused())
}

func TestNewSectionFormModelUnknownSectionHasNoFields(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Nothing", "does-not-exist", &cfg, "")
	assert.Empty(t, m.fields)

	view := m.View()
	assert.Contains(t, view, "Nothing")
	assert.Contains(t, view, "No configurable fields for this section.")
}

func TestNewInput(t *testing.T) {
	in := newInput("placeholder", "value")
	assert.Equal(t, "placeholder", in.Placeholder)
	assert.Equal(t, "value", in.Value())
	assert.Equal(t, 48, in.Width)
	assert.False(t, in.Focused())
}

func TestClampInputWidth(t *testing.T) {
	tests := []struct {
		total, want int
	}{
		{-5, 48},
		{0, 48},
		{10, 16},
		{28, 16},
		{29, 17},
		{60, 48},
		{84, 72},
		{200, 72},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, clampInputWidth(tt.total), "width %d", tt.total)
	}
}

func TestRenderSimpleFields(t *testing.T) {
	text := newInput("", "hello")
	fields := []simpleFormField{
		{label: "Name", desc: "the name", input: text},
		{label: "Enabled", desc: "turn it on", isBool: true, value: true},
		{label: "Disabled", isBool: true},
		{label: "Bare", input: newInput("", "x")},
	}

	out := renderSimpleFields(fields, 1)
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 6)
	assert.True(t, strings.HasPrefix(lines[0], "  Name"))
	assert.Contains(t, lines[0], "the name")
	assert.Contains(t, lines[1], "hello")
	assert.Contains(t, lines[2], "> ")
	assert.Contains(t, lines[2], "Enabled")
	assert.Contains(t, lines[2], "[on]")
	assert.Contains(t, lines[2], "turn it on")
	assert.True(t, strings.HasPrefix(lines[3], "  Disabled"))
	assert.Contains(t, lines[3], "[off]")
	assert.True(t, strings.HasPrefix(lines[4], "  Bare"))
	assert.Contains(t, lines[5], "x")
	assert.False(t, strings.HasSuffix(out, "\n"))

	assert.Empty(t, renderSimpleFields(nil, 0))
}

func TestSectionFormInit(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Scheduler", "scheduler", &cfg, "")
	assert.NotNil(t, m.Init())
}

func TestSectionFormNavigation(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Browser Settings", "browser", &cfg, "")
	last := len(m.fields) - 1
	require.Greater(t, last, 1)

	// Moving up from the first field stays put.
	m, _ = sendForm(t, m, formKey("up"))
	assert.Equal(t, 0, m.cursor)
	assert.True(t, m.fields[0].input.Focused())

	for i, key := range []string{"down", "j", "tab"} {
		m, _ = sendForm(t, m, formKey(key))
		assert.Equal(t, i+1, m.cursor, "after %q", key)
	}
	assert.False(t, m.fields[0].input.Focused(), "leaving a text field blurs it")

	m = moveFormCursor(t, m, last)
	m, _ = sendForm(t, m, formKey("down"))
	assert.Equal(t, last, m.cursor, "moving down from the last field stays put")

	for _, key := range []string{"up", "k", "shift+tab"} {
		before := m.cursor
		m, _ = sendForm(t, m, formKey(key))
		assert.Equal(t, before-1, m.cursor, "after %q", key)
	}

	m = moveFormCursor(t, m, 0)
	assert.True(t, m.fields[0].input.Focused(), "returning to a text field focuses it")
	for i := 1; i < len(m.fields); i++ {
		if !m.fields[i].isBool {
			assert.False(t, m.fields[i].input.Focused(), "field %s", m.fields[i].path)
		}
	}
}

func TestSectionFormSpaceTogglesBoolOnly(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Browser Settings", "browser", &cfg, "")
	headless := formFieldIndex(t, m, "browser.headless")

	m = moveFormCursor(t, m, headless)
	require.False(t, m.fields[headless].value)
	m, cmd := sendForm(t, m, formKey(" "))
	assert.Nil(t, cmd)
	assert.True(t, m.fields[headless].value)
	m, _ = sendForm(t, m, formKey(" "))
	assert.False(t, m.fields[headless].value)

	// Space on a text field is typed into the input instead.
	m = moveFormCursor(t, m, 0)
	before := m.fields[0].input.Value()
	m, _ = sendForm(t, m, formKey(" "))
	assert.Equal(t, before+" ", m.fields[0].input.Value())
}

func TestSectionFormQuitKeys(t *testing.T) {
	cfg := config.Default()
	for _, key := range []string{"ctrl+c", "q", "esc"} {
		m := newSectionFormModel("Browser Settings", "browser", &cfg, "")
		_, cmd := sendForm(t, m, formKey(key))
		require.NotNil(t, cmd, key)
		assert.IsType(t, tea.QuitMsg{}, cmd(), key)
	}
}

func TestSectionFormEditAndSaveWritesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := config.Default()
	require.NoError(t, config.Save(cfgPath, &cfg))

	m := newSectionFormModel("Browser Settings", "browser", &cfg, cfgPath)
	cdp := formFieldIndex(t, m, "browser.cdp_port")
	headless := formFieldIndex(t, m, "browser.headless")
	reuse := formFieldIndex(t, m, "browser.reuse_tabs")
	require.True(t, m.fields[reuse].value, "reuse_tabs defaults on")

	m = moveFormCursor(t, m, cdp)
	assert.True(t, m.fields[cdp].input.Focused())
	m, _ = sendForm(t, m, formKey("ctrl+u"))
	assert.Empty(t, m.fields[cdp].input.Value())
	m, _ = sendForm(t, m, formKey("9333"))
	assert.Equal(t, "9333", m.fields[cdp].input.Value())

	// Enter on a text field that is not the last one does not save.
	m, cmd := sendForm(t, m, formKey("enter"))
	if cmd != nil {
		assert.NotEqual(t, savedMsg{}, cmd())
	}

	m = moveFormCursor(t, m, headless)
	m, _ = sendForm(t, m, formKey(" "))
	m = moveFormCursor(t, m, reuse)
	m, _ = sendForm(t, m, formKey(" "))

	// Values are snapshotted when the command is built, not when it runs.
	m, cmd = sendForm(t, m, formKey("ctrl+s"))
	require.NotNil(t, cmd)
	m.fields[cdp].input.SetValue("1")

	msg := cmd()
	assert.Equal(t, savedMsg{}, msg)
	assert.Equal(t, 9333, cfg.Browser.CDPPort)

	m, cmd = sendForm(t, m, msg)
	assert.Nil(t, cmd)
	assert.True(t, m.saved)
	assert.Contains(t, m.View(), "Saved.")

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, 9333, loaded.Browser.CDPPort)
	assert.True(t, loaded.Browser.Headless)
	assert.Equal(t, "false", config.GetField(loaded, "browser.reuse_tabs"))
}

func TestSectionFormEnterOnLastTextFieldSaves(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "nested", "aviary.yaml")
	cfg := config.Default()
	m := newSectionFormModel("Scheduler Settings", "scheduler", &cfg, cfgPath)
	require.Len(t, m.fields, 1)
	require.True(t, m.fields[0].input.Focused())

	m, _ = sendForm(t, m, formKey("4"))
	_, cmd := sendForm(t, m, formKey("enter"))
	require.NotNil(t, cmd)
	assert.Equal(t, savedMsg{}, cmd())

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "4", config.GetField(loaded, "scheduler.concurrency"))
}

func TestSectionFormSaveInvalidFieldReportsError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := config.Default()
	m := newSectionFormModel("Server Settings", "server", &cfg, cfgPath)
	port := formFieldIndex(t, m, "server.port")

	m = moveFormCursor(t, m, port)
	m, _ = sendForm(t, m, formKey("ctrl+u"))
	m, _ = sendForm(t, m, formKey("abc"))
	m, cmd := sendForm(t, m, formKey("ctrl+s"))
	require.NotNil(t, cmd)

	msg := cmd()
	errText, ok := msg.(errMsg)
	require.True(t, ok, "got %T", msg)
	assert.NotEmpty(t, string(errText))
	_, err := os.Stat(cfgPath)
	assert.True(t, errors.Is(err, os.ErrNotExist), "config must not be written after a field error")

	m, cmd = sendForm(t, m, msg)
	assert.Nil(t, cmd)
	assert.Equal(t, string(errText), m.err)
	assert.False(t, m.saved)
	assert.Contains(t, m.View(), string(errText))
}

func TestSectionFormSaveWriteFailureReportsError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	cfgPath := filepath.Join(blocker, "aviary.yaml")

	cfg := config.Default()
	m := newSectionFormModel("Scheduler Settings", "scheduler", &cfg, cfgPath)
	msg := m.saveCmd()()
	errText, ok := msg.(errMsg)
	require.True(t, ok, "got %T", msg)
	assert.Contains(t, string(errText), "creating config dir")
}

func TestSectionFormWindowResizeClampsTextInputs(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Browser Settings", "browser", &cfg, "")

	m, cmd := sendForm(t, m, tea.WindowSizeMsg{Width: 50, Height: 20})
	assert.Nil(t, cmd)
	assert.Equal(t, 50, m.width)
	for _, f := range m.fields {
		if !f.isBool {
			assert.Equal(t, 38, f.input.Width, f.path)
		}
	}

	m, _ = sendForm(t, m, tea.WindowSizeMsg{Width: 5})
	assert.Equal(t, 16, m.fields[0].input.Width)
}

func TestSectionFormForwardsOtherMessagesToFocusedInput(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Browser Settings", "browser", &cfg, "")

	// Paste arrives as a non-key message the focused input consumes.
	m, _ = sendForm(t, m, textinput.Blink())
	before := m.fields[0].input.Value()
	m, _ = sendForm(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z"), Paste: true})
	assert.Equal(t, before+"z", m.fields[0].input.Value())

	// With a bool field selected, unrelated messages are ignored.
	m = moveFormCursor(t, m, formFieldIndex(t, m, "browser.headless"))
	got, cmd := sendForm(t, m, struct{}{})
	assert.Nil(t, cmd)
	assert.Equal(t, m.cursor, got.cursor)
	_, cmd = sendForm(t, m, formKey("x"))
	assert.Nil(t, cmd)
}

func TestSectionFormView(t *testing.T) {
	cfg := config.Default()
	m := newSectionFormModel("Browser Settings", "browser", &cfg, "")
	view := m.View()

	assert.True(t, strings.HasPrefix(view, "Browser Settings") || strings.Contains(view, "Browser Settings"))
	assert.Contains(t, view, strings.Repeat("─", 24))
	assert.NotContains(t, view, strings.Repeat("─", 73))
	for _, f := range m.fields {
		assert.Contains(t, view, f.label)
	}
	assert.Contains(t, view, "Ctrl+S save")
	assert.NotContains(t, view, "Saved.")

	m.width = 40
	assert.NotContains(t, m.View(), strings.Repeat("─", 39))
}

func TestRunSectionForms(t *testing.T) {
	orig := runFormProgram
	t.Cleanup(func() { runFormProgram = orig })

	var got []sectionFormModel
	runFormProgram = func(m tea.Model) error {
		got = append(got, m.(sectionFormModel))
		return nil
	}

	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, runServerForm(&cfg, cfgPath))
	require.NoError(t, runGeneralForm(&cfg, cfgPath))
	require.NoError(t, runBrowserForm(&cfg, cfgPath))
	require.NoError(t, runSchedulerForm(&cfg, cfgPath))

	require.Len(t, got, 4)
	wantTitles := []string{"Server Settings", "General Settings", "Browser Settings", "Scheduler Settings"}
	wantSections := []string{"server", "general", "browser", "scheduler"}
	for i, m := range got {
		assert.Equal(t, wantTitles[i], m.title)
		assert.Equal(t, cfgPath, m.cfgPath)
		assert.Same(t, &cfg, m.cfg)
		assert.Len(t, m.fields, len(config.SectionFields(wantSections[i])), wantSections[i])
	}

	boom := errors.New("boom")
	runFormProgram = func(tea.Model) error { return boom }
	assert.ErrorIs(t, runServerForm(&cfg, cfgPath), boom)
}
