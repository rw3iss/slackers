// Package theme provides loadable color themes for the slackers TUI.
//
// A theme is a JSON file with a name, mode ("dark" / "light"), and a
// flat map of well-known color variable names to terminal color values
// (256-color indices like "12" or hex strings like "#ff8800").
//
// Themes live in two places:
//   - Embedded defaults shipped with the binary (Default + a few extras).
//   - User-editable themes under $XDG_CONFIG_HOME/slackers/themes/*.json
package theme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Theme is a loadable color theme.
type Theme struct {
	Name   string            `json:"name"`
	Mode   string            `json:"mode"` // "dark" or "light"
	Colors map[string]string `json:"colors"`

	// Path is the on-disk path to the theme file. Empty for built-ins.
	Path string `json:"-"`
	// Builtin indicates the theme came from the embedded set.
	Builtin bool `json:"-"`
}

// Color variable keys used by the renderer. Keep this list in sync with
// styles.go (tui package). New theme files only need to override the keys
// they care about — missing keys fall back to the default theme.
const (
	KeyPrimary       = "primary"
	KeySecondary     = "secondary"
	KeyAccent        = "accent"
	KeyError         = "error"
	KeyMuted         = "muted"
	KeyHighlight     = "highlight"
	KeyMessageText   = "messageText"
	KeyInfoText      = "infoText"
	KeyDayLabel      = "dayLabel"
	KeyTimestamp     = "timestamp"
	KeyBackground    = "background"
	KeyPageHeader    = "pageHeader"
	KeyGroupHeader   = "groupHeader"
	KeyStatusMessage = "statusMessage"
	KeyFileButton    = "fileButton"
	KeyReplyLabel    = "replyLabel"
	KeySelection     = "selection"
	KeyMenuItem      = "menuItem"
	KeyBorderDefault = "borderDefault"
	KeyBorderActive  = "borderActive"
	KeyEmote         = "emote"
	KeyCodeSnippet   = "codeSnippet"
)

// AllKeys is the canonical ordered list of theme color keys.
var AllKeys = []string{
	KeyPrimary, KeySecondary, KeyAccent, KeyError, KeyMuted, KeyHighlight,
	KeyMessageText, KeyInfoText, KeyDayLabel, KeyTimestamp,
	KeyBackground, KeyPageHeader, KeyGroupHeader, KeyStatusMessage,
	KeyFileButton, KeyReplyLabel, KeySelection, KeyMenuItem,
	KeyBorderDefault, KeyBorderActive, KeyEmote, KeyCodeSnippet,
}

// KeyDescription returns a short description of a theme color key.
func KeyDescription(key string) string {
	switch key {
	case KeyPrimary:
		return "Primary accent color"
	case KeySecondary:
		return "Secondary / muted accent"
	case KeyAccent:
		return "Highlights, online status, success"
	case KeyError:
		return "Error and disconnect indicators"
	case KeyMuted:
		return "Muted text and dim labels"
	case KeyHighlight:
		return "Status-bar warning highlight"
	case KeyMessageText:
		return "Default message body text"
	case KeyInfoText:
		return "Placeholder / info text"
	case KeyDayLabel:
		return "Day / date separators in chat"
	case KeyTimestamp:
		return "Message timestamps"
	case KeyBackground:
		return "Pane background fill"
	case KeyPageHeader:
		return "Pane / channel headers"
	case KeyGroupHeader:
		return "Sidebar group headers"
	case KeyStatusMessage:
		return "Status-bar status messages"
	case KeyFileButton:
		return "[FILE:...] file attachments"
	case KeyReplyLabel:
		return "'X replies' labels"
	case KeySelection:
		return "Selected item highlight"
	case KeyMenuItem:
		return "Unselected menu / list item text"
	case KeyBorderDefault:
		return "Default pane border"
	case KeyBorderActive:
		return "Focused pane border"
	case KeyEmote:
		return "Emote action text color"
	case KeyCodeSnippet:
		return "Code snippet text (inline `code` and ```fenced blocks```)"
	}
	return ""
}

// Default returns a copy of the embedded default theme.
func Default() Theme {
	t, err := loadEmbedded("default.json")
	if err != nil {
		// Fallback in case the embed is broken — should never happen.
		return Theme{
			Name: "Default",
			Mode: "dark",
			Colors: map[string]string{
				KeyPrimary:       "12",
				KeySecondary:     "243",
				KeyAccent:        "10",
				KeyError:         "9",
				KeyMuted:         "240",
				KeyHighlight:     "229",
				KeyMessageText:   "252",
				KeyInfoText:      "240",
				KeyDayLabel:      "240",
				KeyTimestamp:     "240",
				KeyBackground:    "",
				KeyPageHeader:    "12",
				KeyGroupHeader:   "240",
				KeyStatusMessage: "240",
				KeyFileButton:    "11",
				KeyReplyLabel:    "10",
				KeySelection:     "12",
				KeyMenuItem:      "252",
				KeyBorderDefault: "243",
				KeyBorderActive:  "12",
			},
		}
	}
	return t
}

// loadEmbedded reads a theme from the embedded builtin/ directory.
func loadEmbedded(name string) (Theme, error) {
	data, err := builtinFS.ReadFile("builtin/" + name)
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return Theme{}, fmt.Errorf("parsing builtin theme %s: %w", name, err)
	}
	t.Builtin = true
	return t, nil
}

// UserDir returns the user's theme directory path.
// It does NOT create the directory.
func UserDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "slackers", "themes")
}

// EnsureUserDir creates the user theme directory if missing.
func EnsureUserDir() error {
	dir := UserDir()
	return os.MkdirAll(dir, 0o755)
}

// LoadAll returns every theme available to the user — built-ins plus any
// JSON files under the user's theme directory. User themes with the same
// name as a built-in override the built-in.
func LoadAll() []Theme {
	seen := map[string]int{}
	var themes []Theme

	// Built-ins first.
	if entries, err := builtinFS.ReadDir("builtin"); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			t, err := loadEmbedded(e.Name())
			if err != nil {
				continue
			}
			seen[strings.ToLower(t.Name)] = len(themes)
			themes = append(themes, t)
		}
	}

	// User themes (override by name).
	if entries, err := os.ReadDir(UserDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(UserDir(), e.Name())
			t, err := Load(path)
			if err != nil {
				continue
			}
			if idx, ok := seen[strings.ToLower(t.Name)]; ok {
				themes[idx] = t
			} else {
				seen[strings.ToLower(t.Name)] = len(themes)
				themes = append(themes, t)
			}
		}
	}

	sort.SliceStable(themes, func(i, j int) bool {
		// Default first, then alphabetical.
		if themes[i].Name == "Default" {
			return true
		}
		if themes[j].Name == "Default" {
			return false
		}
		return strings.ToLower(themes[i].Name) < strings.ToLower(themes[j].Name)
	})
	return themes
}

// Load reads a single theme file from disk.
func Load(path string) (Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return Theme{}, fmt.Errorf("parsing theme %s: %w", path, err)
	}
	t.Path = path
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return t, nil
}

// FindByName looks up a theme by name (case-insensitive).
func FindByName(name string) (Theme, bool) {
	lower := strings.ToLower(name)
	for _, t := range LoadAll() {
		if strings.ToLower(t.Name) == lower {
			return t, true
		}
	}
	return Theme{}, false
}

// Save writes a theme to disk under the user's theme directory. The
// filename is derived from the (sanitized) theme name. Returns the
// resulting path.
func Save(t Theme) (string, error) {
	if err := EnsureUserDir(); err != nil {
		return "", err
	}
	if t.Name == "" {
		return "", errors.New("theme name cannot be empty")
	}
	filename := SanitizeFilename(t.Name) + ".json"
	path := filepath.Join(UserDir(), filename)
	t.Path = path
	t.Builtin = false
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Rename writes the theme under a new name and removes the old file (if it
// was a user theme). Returns the new path. Errors if the destination
// filename already exists.
func Rename(t Theme, newName string) (Theme, error) {
	if newName == "" {
		return t, errors.New("name cannot be empty")
	}
	newFilename := SanitizeFilename(newName) + ".json"
	if newFilename == ".json" {
		return t, errors.New("name produces an invalid filename")
	}
	newPath := filepath.Join(UserDir(), newFilename)
	if t.Path != newPath {
		if _, err := os.Stat(newPath); err == nil {
			return t, fmt.Errorf("theme file %q already exists", newFilename)
		}
	}
	oldPath := t.Path
	t.Name = newName
	t.Path = newPath
	t.Builtin = false
	if err := EnsureUserDir(); err != nil {
		return t, err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return t, err
	}
	if err := os.WriteFile(newPath, data, 0o644); err != nil {
		return t, err
	}
	if oldPath != "" && oldPath != newPath {
		_ = os.Remove(oldPath)
	}
	return t, nil
}

// Delete removes a user theme file from disk. Built-ins cannot be deleted.
func Delete(t Theme) error {
	if t.Builtin || t.Path == "" {
		return errors.New("cannot delete a built-in theme")
	}
	return os.Remove(t.Path)
}

// Clone duplicates a theme into the user theme directory under a new name
// derived from the original. The clone is automatically numbered for
// uniqueness ("Foo Copy 2", "Foo Copy 3", ...) so it never collides.
func Clone(src Theme) (Theme, error) {
	if err := EnsureUserDir(); err != nil {
		return Theme{}, err
	}
	base := src.Name + " Copy"
	name := base
	counter := 2
	for {
		filename := SanitizeFilename(name) + ".json"
		path := filepath.Join(UserDir(), filename)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			// Available — write the clone.
			cloned := Theme{
				Name:   name,
				Mode:   src.Mode,
				Colors: copyColors(src.Colors),
			}
			if _, err := Save(cloned); err != nil {
				return Theme{}, err
			}
			cloned.Path = path
			return cloned, nil
		}
		name = fmt.Sprintf("%s %d", base, counter)
		counter++
		if counter > 1000 {
			return Theme{}, errors.New("could not find a unique clone name")
		}
	}
}

// Get returns the color value for a key, falling back to the default theme
// if the key is missing or empty in t.
func (t Theme) Get(key string) string {
	if v, ok := t.Colors[key]; ok && v != "" {
		return v
	}
	if dv, ok := Default().Colors[key]; ok {
		return dv
	}
	return ""
}

// GetFg returns just the foreground portion of a key's value.
func (t Theme) GetFg(key string) string {
	fg, _ := ParseColor(t.Get(key))
	return fg
}

// GetBg returns just the background portion of a key's value.
func (t Theme) GetBg(key string) string {
	_, bg := ParseColor(t.Get(key))
	return bg
}

// ParseColor splits a theme value into foreground and background parts.
// Theme values support an optional "fg/bg" syntax with optional attribute
// suffixes:
//
//	""           -> fg="",   bg=""    (default)
//	"12"         -> fg="12", bg=""
//	"12/240"     -> fg="12", bg="240"
//	"/240"       -> fg="",   bg="240" (background only)
//	"#ff0/#003"  works the same with hex strings.
//	"12+b"       -> fg="12", bold
//	"12/240+bi"  -> fg="12", bg="240", bold + italic
//
// Attribute flags appear after a "+" and may include 'b' (bold) and 'i'
// (italic). Use ParseColorFull to access them.
func ParseColor(value string) (fg, bg string) {
	fg, bg, _, _ = ParseColorFull(value)
	return fg, bg
}

// ParseColorFull is the same as ParseColor but also returns the bold and
// italic flags.
func ParseColorFull(value string) (fg, bg string, bold, italic bool) {
	if value == "" {
		return "", "", false, false
	}
	colors := value
	if plus := strings.Index(value, "+"); plus >= 0 {
		colors = value[:plus]
		attrs := value[plus+1:]
		bold = strings.ContainsRune(attrs, 'b')
		italic = strings.ContainsRune(attrs, 'i')
	}
	if i := strings.Index(colors, "/"); i >= 0 {
		return colors[:i], colors[i+1:], bold, italic
	}
	return colors, "", bold, italic
}

// JoinColor produces a value string from fg and bg. Empty strings are
// preserved so "/240" (bg only) and "12" (fg only) round-trip correctly.
func JoinColor(fg, bg string) string {
	if bg == "" {
		return fg
	}
	return fg + "/" + bg
}

// JoinColorFull is the same as JoinColor but also encodes bold/italic flags.
func JoinColorFull(fg, bg string, bold, italic bool) string {
	base := JoinColor(fg, bg)
	if !bold && !italic {
		return base
	}
	attrs := ""
	if bold {
		attrs += "b"
	}
	if italic {
		attrs += "i"
	}
	return base + "+" + attrs
}

// IsDark returns true if the theme self-identifies as dark mode.
func (t Theme) IsDark() bool {
	return strings.ToLower(t.Mode) != "light"
}

// DisplayName returns the theme name with a "(light)" suffix
// appended for themes whose mode is "light". Used by the picker
// and the settings overlay so users can tell at a glance which
// themes have a light background. Dark themes get the bare name.
//
// The suffix is purely a presentation concern — the persisted
// cfg.Theme value stays as the canonical Name without the tag,
// so saved configs and FindByName lookups round-trip cleanly.
func (t Theme) DisplayName() string {
	if !t.IsDark() {
		return t.Name + " (light)"
	}
	return t.Name
}

// DisplayNameOf looks up a theme by name and returns its
// DisplayName(), or the input string unchanged if no theme
// matches. Used by surfaces that only have a name string
// (e.g. settings.go's themeValueLabel) and need the same
// "(light)" annotation as the picker.
func DisplayNameOf(name string) string {
	if t, ok := FindByName(name); ok {
		return t.DisplayName()
	}
	return name
}

// SanitizeFilename converts a theme name into a safe filename component.
func SanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func copyColors(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
