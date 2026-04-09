package commands

import (
	"errors"
	"sort"
	"strings"
)

// Registry is the central command dictionary.
//
// Build it once at startup with NewRegistry, register every
// command via Register, and then use Lookup / Get / Run for the
// hot path. The Registry is safe to read concurrently after the
// initial registration phase has completed; mutating it after
// startup (custom commands, runtime emotes) is the caller's
// responsibility to serialise.
type Registry struct {
	byName map[string]*Command
	trie   *trie
}

// NewRegistry returns an empty registry ready to receive
// Register calls.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]*Command),
		trie:   newTrie(),
	}
}

// Register adds a command to the registry. The canonical name
// and every alias are inserted into the trie so prefix lookups
// can match by either. Returns an error if the canonical name
// is already registered (aliases overwrite silently — the most
// recent registration of an alias wins).
func (r *Registry) Register(c Command) error {
	if c.Name == "" {
		return errors.New("commands: cannot register command with empty name")
	}
	c.Name = strings.ToLower(c.Name)
	if _, exists := r.byName[c.Name]; exists {
		return errors.New("commands: duplicate command " + c.Name)
	}
	stored := c
	r.byName[c.Name] = &stored
	r.trie.Insert(c.Name)
	for _, alias := range c.Aliases {
		alias = strings.ToLower(alias)
		if alias == "" {
			continue
		}
		// Aliases also go in byName so Get(alias) works, but the
		// canonical Command struct is shared.
		r.byName[alias] = &stored
		r.trie.Insert(alias)
	}
	return nil
}

// Get returns the command registered under the given canonical
// name or alias, or nil if there's no match. Lookups are
// case-insensitive.
func (r *Registry) Get(name string) *Command {
	return r.byName[strings.ToLower(strings.TrimPrefix(name, "/"))]
}

// All returns every distinct registered command (deduped across
// aliases) sorted alphabetically by canonical name. Used by the
// Command List view.
func (r *Registry) All() []*Command {
	seen := make(map[*Command]struct{}, len(r.byName))
	out := make([]*Command, 0, len(r.byName))
	for _, c := range r.byName {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Lookup returns the top N commands matching the given query,
// ordered by relevance.
//
// The query may include the leading slash; it's stripped before
// matching. The lookup is a two-phase walk:
//
//  1. Trie prefix lookup with the query → candidate names
//  2. If the trie returned nothing (or fewer than topN), fall
//     back to a full-registry fuzzy rescore so subsequence
//     matches like "addfri" → /add-friend still surface
//
// In both cases the final ranking is by FuzzyScore so the
// dropdown order is consistent.
func (r *Registry) Lookup(query string, topN int) []*Command {
	q := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(query)), "/")
	var candidates []string
	if q == "" {
		// No query → return everything alphabetically.
		all := r.All()
		if topN > 0 && len(all) > topN {
			all = all[:topN]
		}
		return all
	}
	candidates = r.trie.Lookup(q)
	if len(candidates) < topN {
		// Pad with a global fuzzy pass so subsequence matches
		// (e.g. "rmfr" → "remove-friend") show up too.
		for name := range r.byName {
			if !contains(candidates, name) {
				candidates = append(candidates, name)
			}
		}
	}
	matches := RankFuzzy(q, candidates)
	// Map names back to Command pointers, dedupe across aliases.
	seen := make(map[*Command]struct{}, len(matches))
	out := make([]*Command, 0, len(matches))
	for _, m := range matches {
		c := r.byName[m.Name]
		if c == nil {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
		if topN > 0 && len(out) >= topN {
			break
		}
	}
	return out
}

// Run looks up the command by the leading token in input and
// invokes its RunFunc. The leading token may be prefixed with
// "/" or not. Returns a Result with StatusError if the command
// isn't registered.
func (r *Registry) Run(input string) Result {
	input = strings.TrimSpace(input)
	if input == "" {
		return Result{Status: StatusError, StatusBar: "Empty command"}
	}
	input = strings.TrimPrefix(input, "/")
	// Split on the first whitespace run.
	name := input
	rest := ""
	if i := strings.IndexAny(input, " \t"); i >= 0 {
		name = input[:i]
		rest = strings.TrimSpace(input[i+1:])
	}
	cmd := r.Get(name)
	if cmd == nil {
		return Result{
			Status:    StatusError,
			StatusBar: "Unknown command: /" + name,
		}
	}
	ctx := &Context{
		Args: tokenize(rest),
		Raw:  rest,
	}
	if cmd.Run == nil {
		return Result{
			Status:    StatusError,
			StatusBar: "/" + name + " has no handler",
		}
	}
	return cmd.Run(ctx)
}

// Count returns the number of distinct registered commands
// (excluding aliases).
func (r *Registry) Count() int {
	seen := make(map[*Command]struct{}, len(r.byName))
	for _, c := range r.byName {
		seen[c] = struct{}{}
	}
	return len(seen)
}

// tokenize splits a raw arg string into tokens, honouring
// double-quoted runs as single tokens. Backslash escapes are not
// supported — quotes inside JSON values should use Go's standard
// `\"` escape which we don't strip.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	var (
		out      []string
		buf      strings.Builder
		inQuotes bool
	)
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case (r == ' ' || r == '\t') && !inQuotes:
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
