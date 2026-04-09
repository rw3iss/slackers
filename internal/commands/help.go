package commands

import (
	"embed"
	"sort"
	"strings"
)

// Help files are embedded at build time from the project-root /help
// directory. The TUI host loads them via Topics() / Topic() so the
// /help command can render them in the Output view.
//
// To add a new topic: drop a markdown file at help/<topic>.md and
// rebuild — the embed.FS picks it up automatically.

//go:embed help/*.md
var helpFS embed.FS

// helpDir is the embed sub-directory we read from. Kept as a const
// so the path is in one place if it ever moves.
const helpDir = "help"

// Topics returns the list of available help topic names (without
// the .md extension), sorted alphabetically. The "main" topic is
// always first if present.
func Topics() []string {
	entries, err := helpFS.ReadDir(helpDir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".md"))
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Pin "main" to the top.
		if out[i] == "main" {
			return true
		}
		if out[j] == "main" {
			return false
		}
		return out[i] < out[j]
	})
	return out
}

// Topic returns the markdown body for the given topic name (without
// extension), or empty string + ok=false if the file doesn't exist.
func Topic(name string) (string, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		name = "main"
	}
	data, err := helpFS.ReadFile(helpDir + "/" + name + ".md")
	if err != nil {
		return "", false
	}
	return string(data), true
}
