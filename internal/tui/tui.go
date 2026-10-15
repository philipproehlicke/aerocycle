// Package tui is an interactive editor for aerocycle's shortcut config.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aerocycle/internal/config"
	"aerocycle/internal/keymap"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	appStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
)

type appEntry struct{ bundle, name string }

type binding struct{ key, bundle, name string }

// appItem adapts an app to bubbles/list.
type appItem struct{ appEntry }

func (a appItem) Title() string       { return a.name }
func (a appItem) Description() string { return a.bundle }
func (a appItem) FilterValue() string { return a.name + " " + a.bundle }

type mode int

const (
	modeList mode = iota
	modeAddKey
	modePickApp
	modeAddBundle
)

type model struct {
	cfgPath  string
	timeout  int
	bindings []binding
	apps     []appEntry

	mode     mode
	cursor   int
	keyIn    textinput.Model
	bundleIn textinput.Model
	appList  list.Model

	status string
	isErr  bool
	w, h   int
	done   bool
}

// Run opens the config TUI.
func Run() error {
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		cfg = &config.Config{CycleTimeoutMS: 1200, Apps: map[string]string{}}
	}
	m := newModel(path, cfg, loadRunningApps())
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newModel(cfgPath string, cfg *config.Config, apps []appEntry) model {
	names := map[string]string{}
	for _, a := range apps {
		names[a.bundle] = a.name
	}

	bs := make([]binding, 0, len(cfg.Apps))
	for k, b := range cfg.Apps {
		bs = append(bs, binding{key: k, bundle: b, name: names[b]})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].key < bs[j].key })

	ki := textinput.New()
	ki.Placeholder = "single key, e.g. d"
	ki.CharLimit = 1
	ki.Width = 16

	bi := textinput.New()
	bi.Placeholder = "com.example.App"
	bi.CharLimit = 128
	bi.Width = 48

	return model{cfgPath: cfgPath, timeout: cfg.CycleTimeoutMS, bindings: bs, apps: apps, keyIn: ki, bundleIn: bi}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeAddKey:
			return m.updateAddKey(msg)
		case modeAddBundle:
			return m.updateAddBundle(msg)
		case modePickApp:
			return m.updatePickApp(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.done = true
		return m, tea.Quit
	case "a":
		m.mode = modeAddKey
		m.keyIn.SetValue("")
		m.keyIn.Focus()
		m.status, m.isErr = "", false
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.bindings)-1 {
			m.cursor++
		}
	case "x", "d", "delete", "backspace":
		if len(m.bindings) > 0 {
			m.bindings = append(m.bindings[:m.cursor], m.bindings[m.cursor+1:]...)
			if m.cursor >= len(m.bindings) && m.cursor > 0 {
				m.cursor--
			}
			m.status, m.isErr = "removed (press s to save)", false
		}
	case "s":
		m.save()
	case "r":
		if cfg, err := config.Load(m.cfgPath); err == nil {
			m.timeout = cfg.CycleTimeoutMS
			m.bindings = bindingsFrom(cfg, m.apps)
			m.cursor = 0
			m.status, m.isErr = "reloaded from disk", false
		} else {
			m.status, m.isErr = err.Error(), true
		}
	}
	return m, nil
}

func (m model) updateAddKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.status = modeList, ""
		return m, nil
	case "enter":
		k := strings.ToLower(strings.TrimSpace(m.keyIn.Value()))
		if k == "" {
			m.status, m.isErr = "type a key", true
			return m, nil
		}
		if !keymap.Valid(k) {
			m.status, m.isErr = fmt.Sprintf("%q is not a valid key", k), true
			return m, nil
		}
		m.mode = modePickApp
		m.initAppList()
		m.status = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.keyIn, cmd = m.keyIn.Update(msg)
	return m, cmd
}

func (m model) updateAddBundle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modePickApp
		return m, nil
	case "enter":
		b := strings.TrimSpace(m.bundleIn.Value())
		if b == "" {
			m.status, m.isErr = "type a bundle id", true
			return m, nil
		}
		m.addBinding(m.keyIn.Value(), b)
		return m, nil
	}
	var cmd tea.Cmd
	m.bundleIn, cmd = m.bundleIn.Update(msg)
	return m, cmd
}

func (m model) updatePickApp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeAddKey
		m.keyIn.Focus()
		return m, nil
	case "enter":
		if it, ok := m.appList.SelectedItem().(appItem); ok {
			if it.bundle == "" {
				m.mode = modeAddBundle
				m.bundleIn.SetValue("")
				m.bundleIn.Focus()
				return m, nil
			}
			m.addBinding(m.keyIn.Value(), it.bundle)
			return m, nil
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.appList, cmd = m.appList.Update(msg)
	return m, cmd
}

func (m *model) initAppList() {
	items := make([]list.Item, 0, len(m.apps)+1)
	for _, a := range m.apps {
		items = append(items, appItem{a})
	}
	items = append(items, appItem{appEntry{name: "\u270e enter bundle id manually"}})

	h := m.h - 8
	if h < 5 {
		h = 5
	}
	w := m.w - 8
	if w < 24 {
		w = 24
	}
	l := list.New(items, list.NewDefaultDelegate(), w, h)
	l.Title = "Pick an app for right-\u2318+" + m.keyIn.Value()
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	m.appList = l
}

func (m *model) addBinding(key, bundle string) {
	key = strings.ToLower(strings.TrimSpace(key))
	name := m.nameFor(bundle)
	for i := range m.bindings {
		if m.bindings[i].key == key {
			m.bindings[i] = binding{key: key, bundle: bundle, name: name}
			m.mode, m.status, m.isErr = modeList, "updated right-\u2318+"+key+" (press s to save)", false
			return
		}
	}
	m.bindings = append(m.bindings, binding{key: key, bundle: bundle, name: name})
	sort.Slice(m.bindings, func(i, j int) bool { return m.bindings[i].key < m.bindings[j].key })
	m.mode, m.status, m.isErr = modeList, "added right-\u2318+"+key+" (press s to save)", false
}

func (m model) nameFor(bundle string) string {
	for _, a := range m.apps {
		if a.bundle == bundle {
			return a.name
		}
	}
	return ""
}

func (m *model) save() {
	cfg := &config.Config{CycleTimeoutMS: m.timeout, Apps: map[string]string{}}
	for _, b := range m.bindings {
		cfg.Apps[b.key] = b.bundle
	}
	if err := config.Save(m.cfgPath, cfg); err != nil {
		m.status, m.isErr = err.Error(), true
		return
	}
	if reloadDaemon() {
		m.status, m.isErr = "saved \u2014 daemon reloaded", false
	} else {
		m.status, m.isErr = "saved \u2014 restart aerocycle to apply", false
	}
}

func (m model) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("aerocycle") + subtleStyle.Render("  right-\u2318 shortcuts") + "\n")
	b.WriteString(subtleStyle.Render(m.cfgPath) + "\n\n")

	switch m.mode {
	case modeList:
		if len(m.bindings) == 0 {
			b.WriteString(subtleStyle.Render("no shortcuts yet \u2014 press 'a' to add one") + "\n")
		}
		for i, bd := range m.bindings {
			name := bd.name
			if name == "" {
				name = bd.bundle
			}
			line := fmt.Sprintf("right-\u2318+%s   %s  %s", bd.key, appStyle.Render(pad(name, 20)), subtleStyle.Render(bd.bundle))
			if i == m.cursor {
				b.WriteString(selectedStyle.Render("\u276f ") + line + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString("\n" + subtleStyle.Render("a add \u00b7 d delete \u00b7 s save \u00b7 r reload \u00b7 q quit") + "\n")

	case modeAddKey:
		b.WriteString(titleStyle.Render("New shortcut") + "\n\n")
		b.WriteString("key to press with right-\u2318:\n" + m.keyIn.View() + "\n\n")
		b.WriteString(subtleStyle.Render("valid keys: "+strings.Join(keymap.Names(), " ")) + "\n\n")
		b.WriteString(subtleStyle.Render("enter confirm \u00b7 esc back") + "\n")

	case modeAddBundle:
		b.WriteString(titleStyle.Render("Bundle id") + "\n\n")
		b.WriteString(m.bundleIn.View() + "\n\n")
		b.WriteString(subtleStyle.Render("enter confirm \u00b7 esc back") + "\n")

	case modePickApp:
		b.WriteString(m.appList.View() + "\n")
		b.WriteString(subtleStyle.Render("\u2191/\u2193 move \u00b7 / filter \u00b7 enter choose \u00b7 esc back") + "\n")
	}

	if m.status != "" {
		b.WriteString("\n")
		if m.isErr {
			b.WriteString(errStyle.Render("\u2717 " + m.status))
		} else {
			b.WriteString(okStyle.Render("\u2713 " + m.status))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func bindingsFrom(cfg *config.Config, apps []appEntry) []binding {
	names := map[string]string{}
	for _, a := range apps {
		names[a.bundle] = a.name
	}
	bs := make([]binding, 0, len(cfg.Apps))
	for k, b := range cfg.Apps {
		bs = append(bs, binding{key: k, bundle: b, name: names[b]})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].key < bs[j].key })
	return bs
}

func loadRunningApps() []appEntry {
	out, err := exec.Command("aerospace", "list-apps").Output()
	if err != nil {
		return nil
	}
	var apps []appEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		apps = append(apps, appEntry{
			bundle: strings.TrimSpace(parts[1]),
			name:   strings.TrimSpace(parts[2]),
		})
	}
	return apps
}

// reloadDaemon asks a running aerocycle to reload its config (SIGHUP).
func reloadDaemon() bool {
	raw, err := os.ReadFile(config.PidPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, syscall.SIGHUP) == nil
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
