// Package emotes manages the emote dictionary: embedded defaults,
// user-local overrides, variable expansion, and persistence.
//
// Emotes are slash commands (/laugh, /wave, /emote <text>) that
// produce formatted action text like "Ryan laughs." where Ryan is
// the sender's display name. Each emote is a template string with
// $variable placeholders resolved at send time.
//
// # Dictionary layers
//
//   - Defaults: embedded defaults.json shipped with the binary.
//     Contains ~25 standard emotes. Cannot be deleted by the user.
//   - Custom: ~/.config/slackers/emotes.json. User-defined emotes
//     plus overrides of defaults. Wins on key conflict.
//   - Merged: defaults + custom. The live dictionary used by the
//     command system and the UI.
//
// # Variables
//
//   - $sender / $me — the sender's display name
//   - $receiver / $you — the receiver's display name (empty in groups)
//   - $all — comma-separated list of all participants
//   - $text — free-form text (only for the generic /emote command)
package emotes

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed defaults.json
var defaultsJSON []byte

// Emote is a single entry in the dictionary.
type Emote struct {
	Command  string `json:"command"`
	Text     string `json:"text"`
	IsCustom bool   `json:"-"` // true if from user file or overriding a default
}

// Store holds the three-layer emote dictionary.
type Store struct {
	defaults map[string]Emote
	custom   map[string]Emote
	merged   map[string]Emote
	path     string // user emotes.json path
}

// NewStore loads the embedded defaults and the user's custom
// emotes file (tolerant of missing/empty), merges them, and
// returns a ready-to-use store.
func NewStore(configDir string) *Store {
	s := &Store{
		defaults: make(map[string]Emote),
		custom:   make(map[string]Emote),
		merged:   make(map[string]Emote),
		path:     filepath.Join(configDir, "emotes.json"),
	}
	// Load embedded defaults.
	var raw map[string]string
	if err := json.Unmarshal(defaultsJSON, &raw); err == nil {
		for cmd, text := range raw {
			s.defaults[cmd] = Emote{Command: cmd, Text: text}
		}
	}
	// Load user custom file (tolerant of missing).
	s.loadCustom()
	s.merge()
	return s
}

func (s *Store) loadCustom() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // missing or unreadable — that's fine
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for cmd, text := range raw {
		s.custom[cmd] = Emote{Command: cmd, Text: text, IsCustom: true}
	}
}

func (s *Store) merge() {
	s.merged = make(map[string]Emote, len(s.defaults)+len(s.custom))
	for k, v := range s.defaults {
		s.merged[k] = v
	}
	for k, v := range s.custom {
		v.IsCustom = true
		s.merged[k] = v
	}
}

// All returns every emote in the merged dictionary, sorted
// alphabetically by command name.
func (s *Store) All() []Emote {
	out := make([]Emote, 0, len(s.merged))
	for _, e := range s.merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Command < out[j].Command
	})
	return out
}

// Get returns the merged emote for the given command, or false.
func (s *Store) Get(command string) (Emote, bool) {
	e, ok := s.merged[strings.ToLower(command)]
	return e, ok
}

// Defaults returns emotes from the built-in set that are NOT
// overridden by a custom entry. Sorted alphabetically.
func (s *Store) Defaults() []Emote {
	out := make([]Emote, 0, len(s.defaults))
	for k, v := range s.defaults {
		if _, overridden := s.custom[k]; !overridden {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Command < out[j].Command
	})
	return out
}

// Custom returns only the user-defined emotes (including
// overrides of defaults). Sorted alphabetically.
func (s *Store) Custom() []Emote {
	out := make([]Emote, 0, len(s.custom))
	for _, v := range s.custom {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Command < out[j].Command
	})
	return out
}

// IsDefault reports whether the command name exists in the
// built-in defaults (regardless of whether it's overridden).
func (s *Store) IsDefault(command string) bool {
	_, ok := s.defaults[strings.ToLower(command)]
	return ok
}

// Set adds or updates an emote in the custom layer, re-merges,
// and persists to the user file.
func (s *Store) Set(e Emote) error {
	e.Command = strings.ToLower(strings.TrimSpace(e.Command))
	if e.Command == "" {
		return errors.New("emote command cannot be empty")
	}
	if strings.TrimSpace(e.Text) == "" {
		return errors.New("emote text cannot be empty")
	}
	e.IsCustom = true
	s.custom[e.Command] = e
	s.merge()
	return s.save()
}

// Delete removes an emote from the custom layer. If the emote
// was overriding a default, the default resurfaces in the merged
// dictionary. Built-in defaults cannot be deleted.
func (s *Store) Delete(command string) error {
	command = strings.ToLower(command)
	if _, ok := s.custom[command]; !ok {
		return fmt.Errorf("emote /%s is not a custom emote", command)
	}
	delete(s.custom, command)
	s.merge()
	return s.save()
}

func (s *Store) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw := make(map[string]string, len(s.custom))
	for k, v := range s.custom {
		raw[k] = v.Text
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Count returns the number of emotes in the merged dictionary.
func (s *Store) Count() int {
	return len(s.merged)
}

// validVars is the set of variable names recognized by the
// template expander. Anything else is an error.
var validVars = map[string]bool{
	"sender":   true,
	"me":       true,
	"receiver": true,
	"you":      true,
	"all":      true,
	"text":     true,
}

var varPattern = regexp.MustCompile(`\$([a-zA-Z_]+)`)

// ValidateTemplate checks that every $variable in the text is
// from the recognized set. Returns nil if valid.
func ValidateTemplate(text string) error {
	matches := varPattern.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.ToLower(m[1])
		if !validVars[name] {
			return fmt.Errorf("unknown variable $%s (valid: $sender, $me, $receiver, $you, $all, $text)", m[1])
		}
	}
	return nil
}

// FormatText resolves all $variables in the emote template and
// returns the final text ready to display.
//
// Parameters:
//   - command: the emote command name (used to look up the template)
//   - sender: the sender's display name ($sender / $me)
//   - receiver: the receiver's display name ($receiver / $you)
//   - all: comma-separated participant list ($all)
//   - freeText: the raw text after /emote ($text)
func (s *Store) FormatText(command, sender, receiver, all, freeText string) (string, error) {
	e, ok := s.Get(command)
	if !ok {
		return "", fmt.Errorf("emote /%s not found", command)
	}
	return ExpandTemplate(e.Text, sender, receiver, all, freeText), nil
}

// ExpandTemplate resolves variables in a template string.
func ExpandTemplate(tmpl, sender, receiver, all, freeText string) string {
	r := strings.NewReplacer(
		"$sender", sender,
		"$me", sender,
		"$receiver", receiver,
		"$you", receiver,
		"$all", all,
		"$text", freeText,
	)
	return r.Replace(tmpl)
}
