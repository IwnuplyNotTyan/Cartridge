package ui

import (
	"charm.land/bubbles/v2/key"

	"cartridge/internal/i18n"
)

// Keys, panes and row types for the browser columns.
// Pane is the focused column.
type Pane int

// keyMap wires real keybindings (used both for input and the help view).
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Prev     key.Binding
	Next     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Select   key.Binding
	Rescan   key.Binding
	Launch   key.Binding
	Settings key.Binding
	Copy     key.Binding
	Deploy   key.Binding
	Search   key.Binding
	Favorite key.Binding
	Alt      key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", i18n.T("key.up"))),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", i18n.T("key.down"))),
		Prev:     key.NewBinding(key.WithKeys("shift+tab", "h", "left"), key.WithHelp("←/h", i18n.T("key.pane_left"))),
		Next:     key.NewBinding(key.WithKeys("tab", "l", "right"), key.WithHelp("tab", i18n.T("key.pane_right"))),
		Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", i18n.T("key.first"))),
		Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", i18n.T("key.last"))),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", i18n.T("key.page_up"))),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", i18n.T("key.page_down"))),
		Select:   key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", i18n.T("key.open"))),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", i18n.T("key.search"))),
		Rescan:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", i18n.T("key.rescan"))),
		Launch:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", i18n.T("key.launch"))),
		Settings: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", i18n.T("key.settings"))),
		Copy:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", i18n.T("key.copy"))),
		Deploy:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", i18n.T("key.deploy"))),
		Favorite: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", i18n.T("key.favorite"))),
		Alt:      key.NewBinding(key.WithKeys("n", "p"), key.WithHelp("n/p", i18n.T("key.alt"))),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", i18n.T("key.help"))),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", i18n.T("key.quit"))),
	}
}

// pluralWord picks the localized noun form for n ("word.rom" → ROM/ROMs…).
func pluralWord(n int, key string) string {
	return i18n.Word(n, i18n.T(key+".one"), i18n.T(key+".few"), i18n.T(key+".many"))
}

// ShortHelp implements help.KeyMap (unused: only full help is shown).
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Help, k.Quit} }

// FullHelp implements help.KeyMap.
// NB: bubbles renders each inner slice as a COLUMN, so keep exactly 4:
// more columns widen every help line and break narrow screens.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Prev, k.Next, k.Select, k.Launch},
		{k.PageUp, k.PageDown, k.Rescan, k.Settings},
		{k.Copy, k.Deploy, k.Search, k.Favorite, k.Alt, k.Help, k.Quit},
	}
}

// ConsoleRow is one entry of the left column.
type ConsoleRow struct {
	ID    string
	Name  string
	Short string
	Count int
}

// metaRow is one navigable metadata row: copy holds the clipboard text,
// kind is "recipe"/"core" on switchable rows, "cheats" on the summary row,
// "cheat" on one expanded cheat (cheat = its index), "mods" on the mods
// summary row, "mod" on one expanded mod file (enter plays it via a
// hard-patched instance, d toggles the softpatch copy), "sep" on separators.
type metaRow struct {
	kind  string
	label string
	lines []string
	copy  string
	sep   bool
	cheat int
}
