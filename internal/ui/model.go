package ui

import (
	"time"

	"charm.land/bubbles/v2/help"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/exp/charmtone"

	"cartridge/internal/cheats"
	"cartridge/internal/config"
	"cartridge/internal/emulate"
	"cartridge/internal/favorites"
	"cartridge/internal/i18n"
	"cartridge/internal/mods"
	"cartridge/internal/overrides"
	"cartridge/internal/playtime"
	"cartridge/internal/rom"
)

// Package ui implements the 3-column cartridge browser:
// consoles | games | metadata, separated by grey dividers,
// with bubbles full help above the bottom info bar on "?".
type rescanMsg struct {
	games []rom.Game
	errs  []error
}

// Model is the bubbletea root model.
type Model struct {
	games    []rom.Game
	rows     []ConsoleRow // rows[0] is always Home; consCur == len(rows) is the settings entry
	consCur  int
	gameCur  int
	filtered []rom.Game
	shelf    []shelfRow // games pane display rows (headers, blanks, games)
	// Search state: query is the committed filter (session-only),
	// searching + searchBuf is the live input line above the info bar.
	query     string
	searching bool
	searchBuf []rune

	focus    Pane
	meta     viewport.Model
	metaCur  int       // cursor over metaRows (separators skipped)
	metaRows []metaRow // navigable metadata rows for copying/switching
	// Cheat list state: cached file per game+path, expansion flag.
	// cheatPile is a pile source to materialize a missing live path from.
	cheatKey     string
	cheatPath    string
	cheatPile    string
	cheatStat    cheatFileStat
	cheatFile    *cheats.File
	cheatMissing bool
	cheatsOpen   bool
	// Mod list state: cached files per game+recipe+dirs, expansion flag.
	// modTarget maps a pile file to the exact deployed path RetroArch
	// autopatches by (game stem + ext, numbered on collision);
	// modActive marks slots whose bytes already sit in place.
	modKey    string
	modLive   string
	modFiles  []mods.Mod
	modTarget map[string]string
	modActive map[string]bool
	modBuilt  map[string]bool
	modCheck  map[string]mods.Check
	modStat   modCacheStat
	modsOpen  bool
	// artOpen expands the cached artwork section (icon/wide/hero/logo rows,
	// enter opens the file with the desktop default app).
	artOpen bool
	// Background run state: the emulator keeps going while the TUI
	// stays up; runView shows its log screen, runCh streams lines.
	run       *gameRun
	runCh     chan string
	runView   bool
	runScroll int
	followRun bool
	keys      keyMap
	help      help.Model
	showHelp  bool

	width  int
	height int
	ready  bool

	romDir  string
	libPath string
	status  string
	// statusToken versions bar messages: expiry timers only clear the
	// message they were scheduled for (see status.go).
	statusToken int

	recipes []emulate.Recipe
	// overrides persists per-game emulator/core choices from metadata (n/p).
	overrides *overrides.Store
	play      *playtime.Store
	// favorites persists favorite game IDs pinned above the consoles list.
	favorites *favorites.Store

	// Settings panel state. Settings live in the left column under the
	// consoles: hovering the settings row swaps games+metadata for the
	// panel, settActive means the focus is inside it.
	settActive  bool
	settTab     SettingsTab
	settCur     int
	settEditing bool
	settBuf     []rune
	// settPatchField selects which recipe patch field is being edited
	// in the Recipes tab ("emulator", "args", "consoles", ...).
	settPatchField string
	// settField is the detail-field cursor under the selected recipe:
	// -1 means the recipe row itself, 0..N-1 means one of the
	// recipeFields() rows shown directly under the selected recipe.
	settField int

	cfgFile     string
	recipesPath string
	patchesPath string
	modsDir     string
	patchedDir  string
	cheatsDir   string
	langRaw     string
	// showArt toggles the poster preview (ascii/half-block art) in the
	// metadata pane. True by default; flipped from the General tab.
	showArt bool
	// openApps is the default for recipes that do not force their
	// own `open` setting.
	openApps bool
	steam    config.SteamConfig

	baseRecipes []emulate.Recipe
	patches     []emulate.RecipePatch

	// Icons tab state: async bulk-fetch progress plus cached cache stats
	// (refreshed on entering the tab and after actions, never per render).
	iconsCh        chan iconsProgMsg
	iconsActive    bool
	iconsTotal     int
	iconsDone      int
	iconsOk        int
	iconsFail      int
	iconsFiles     int
	iconsBytes     int64
	iconsMiss      int
	iconsCached    int
	iconsWides     int
	iconsHeroes    int
	iconsLogos     int
	iconsStatFresh bool

	// Mouse state: last left-click target + time for double-click
	// detection (single click selects, second click on the same row
	// within the interval activates). Values are opaque ids built by
	// the mouse handler (see mouse.go).
	mouseLastID string
	mouseLastAt time.Time
}

// launchDoneMsg reports an emulator run back to the model.
type launchDoneMsg struct {
	gameID string
	dur    time.Duration
	err    error
}

// Option configures the model.
type Option func(*Model)

// WithRecipes sets emulator launch recipes.
func WithRecipes(r []emulate.Recipe) Option {
	return func(m *Model) { m.recipes = r }
}

// WithPlaytime sets the play-time store.
func WithPlaytime(s *playtime.Store) Option {
	return func(m *Model) { m.play = s }
}

// WithOverrides sets the per-game emulator/core choice store.
func WithOverrides(s *overrides.Store) Option {
	return func(m *Model) { m.overrides = s }
}

// WithFavorites sets the favorite games store shown on the home page.
func WithFavorites(s *favorites.Store) Option {
	return func(m *Model) { m.favorites = s }
}

// WithBaseRecipes keeps the unpatched base recipes for the settings view.
func WithBaseRecipes(r []emulate.Recipe) Option {
	return func(m *Model) { m.baseRecipes = r }
}

// WithPatches sets recipe overlay patches (never rewrite the base file).
func WithPatches(p []emulate.RecipePatch) Option {
	return func(m *Model) { m.patches = p }
}

// WithPaths sets config file location, recipes/patches paths and data dirs.
func WithPaths(cfgFile, recipesPath, patchesPath, modsDir, cheatsDir string) Option {
	return func(m *Model) {
		m.cfgFile = cfgFile
		m.recipesPath = recipesPath
		m.patchesPath = patchesPath
		m.modsDir = modsDir
		m.cheatsDir = cheatsDir
	}
}

// WithPatchedDir sets where hard-patched ROM instances live.
func WithPatchedDir(dir string) Option {
	return func(m *Model) { m.patchedDir = dir }
}

// WithLangRaw remembers the raw language value from config ("auto"/"en"/"ru").
func WithLangRaw(raw string) Option {
	return func(m *Model) { m.langRaw = raw }
}

// WithShowArt sets the poster preview toggle (ascii/half-block art).
func WithShowArt(v bool) Option {
	return func(m *Model) { m.showArt = v }
}

// WithOpenApps sets the default for recipes that do not force `open`.
func WithOpenApps(open bool) Option {
	return func(m *Model) { m.openApps = open }
}

// openAppsEnabled reports the global `open -a` default.
func (m Model) openAppsEnabled() bool { return m.openApps }

// WithSteam sets the Steam integration settings (auto-add, categories).
func WithSteam(s config.SteamConfig) Option {
	return func(m *Model) { m.steam = s }
}

// New builds the model from an initial scan.
func New(games []rom.Game, romDir, libPath string, opts ...Option) Model {
	m := Model{
		romDir:  romDir,
		libPath: libPath,
		meta:    viewport.New(0, 0),
		keys:    newKeyMap(),
		help:    help.New(),
		showArt: true,
	}
	m.help.ShowAll = true
	// NB: help comes from bubbles v2, so its styles use charm.land/lipgloss/v2
	// where Color is a constructor func, not a type.
	m.help.Styles.ShortKey = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Hazy.Hex()))
	m.help.Styles.ShortDesc = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Squid.Hex()))
	m.help.Styles.ShortSeparator = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	m.help.Styles.FullKey = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Hazy.Hex()))
	m.help.Styles.FullDesc = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Squid.Hex()))
	m.help.Styles.FullSeparator = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	m.help.Styles.Ellipsis = lipglossv2.NewStyle().Foreground(lipglossv2.Color(charmtone.Iron.Hex()))
	for _, opt := range opts {
		opt(&m)
	}
	if len(m.baseRecipes) == 0 && len(m.recipes) > 0 {
		m.baseRecipes = append([]emulate.Recipe(nil), m.recipes...)
	}
	if m.langRaw == "" {
		m.langRaw = "auto"
	}
	if m.steam.CategoryTemplate == "" && m.steam.Prefix == "" && !m.steam.AutoAdd && len(m.steam.Users) == 0 && m.steam.Exe == "" {
		m.steam = config.DefaultSteamConfig()
	} else if m.steam.CategoryTemplate == "" {
		m.steam.CategoryTemplate = config.DefaultSteamConfig().CategoryTemplate
	}
	m.setGames(games)
	return m
}

func (m *Model) setGames(games []rom.Game) {
	// Drop stale rows that are not valid games (mod files, archive junk
	// from older scans lingering in the library): a rescan cleans both
	// the view and the saved library at once.
	filtered := make([]rom.Game, 0, len(games))
	for _, g := range games {
		if rom.ValidGame(g) {
			filtered = append(filtered, g)
		}
	}
	m.games = filtered
	counts := map[string]int{}
	names := map[string]string{}
	for _, g := range filtered {
		counts[g.ConsoleID]++
		names[g.ConsoleID] = g.ConsoleName
	}
	m.rows = []ConsoleRow{{ID: "all", Name: i18n.T("row.all"), Count: len(filtered)}}
	for _, c := range rom.Consoles {
		if n, ok := counts[c.ID]; ok {
			m.rows = append(m.rows, ConsoleRow{ID: c.ID, Name: c.Name, Short: c.Short, Count: n})
		}
	}
	// Keep unknown consoles (if any) at the end instead of dropping them.
	if n, ok := counts["unknown"]; ok {
		m.rows = append(m.rows, ConsoleRow{ID: "unknown", Name: i18n.T("row.unknown"), Short: "???", Count: n})
	}
	if m.consCur > len(m.rows) {
		m.consCur = len(m.rows)
	}
	m.gameCur = 0
	m.refilter()
}
