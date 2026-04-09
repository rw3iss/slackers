package commands

import (
	"sort"
	"strings"
)

// trieNode is a single character node in the command-name index.
//
// Each registered command's name is decomposed into runes and
// inserted as a path from the root. cmdNames at a given node lists
// every command name whose prefix matches the path from the root
// to that node, allowing prefix lookups to return all matching
// commands without re-scanning the registry.
//
// We index by lowercased rune so lookups are case-insensitive,
// which matches how slash commands are usually typed.
type trieNode struct {
	children map[rune]*trieNode
	// cmdNames is the set of command names whose first len(path)
	// characters match this node's path. Stored as a slice (not a
	// set) because we need a stable, alphabetical iteration order
	// for the suggestion popup.
	cmdNames []string
}

func newTrieNode() *trieNode {
	return &trieNode{children: make(map[rune]*trieNode)}
}

// trie is the prefix index over all registered command names.
//
// Insertion is O(len(name)). Lookup of all commands matching a
// given prefix is O(len(prefix)) regardless of how many commands
// are registered, which is exactly what we want for the typing
// hot path: as the user enters each character we re-walk the trie
// from the root and return the cached subtree leaves.
type trie struct {
	root *trieNode
}

func newTrie() *trie {
	return &trie{root: newTrieNode()}
}

// Insert adds a command name into the index. The same command can
// be inserted under multiple names (canonical + aliases) and the
// caller is responsible for deduping at the registry layer.
func (t *trie) Insert(name string) {
	name = strings.ToLower(name)
	if name == "" {
		return
	}
	node := t.root
	for _, r := range name {
		child, ok := node.children[r]
		if !ok {
			child = newTrieNode()
			node.children[r] = child
		}
		node = child
	}
	// Append the name to every node along the path so prefix
	// lookups can return matching commands without descending
	// the subtree at query time.
	node = t.root
	for _, r := range name {
		node = node.children[r]
		node.cmdNames = append(node.cmdNames, name)
	}
}

// Lookup returns every command name whose first runes match the
// given prefix, sorted alphabetically. An empty prefix returns
// every name in the index.
//
// Returns a freshly-allocated slice so callers can sort or
// truncate without disturbing the cached lists on trie nodes.
func (t *trie) Lookup(prefix string) []string {
	prefix = strings.ToLower(prefix)
	if prefix == "" {
		// Empty prefix → walk the entire root cmdNames union.
		// Each node has the names of EVERY descendant, so the
		// root holds all of them.
		out := dedupeAndSort(allNames(t.root))
		return out
	}
	node := t.root
	for _, r := range prefix {
		child, ok := node.children[r]
		if !ok {
			return nil
		}
		node = child
	}
	out := append([]string(nil), node.cmdNames...)
	return dedupeAndSort(out)
}

// allNames walks the entire trie and returns every command name
// at every leaf. Used by the empty-prefix case in Lookup.
func allNames(root *trieNode) []string {
	if root == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var walk func(*trieNode)
	walk = func(n *trieNode) {
		for _, name := range n.cmdNames {
			seen[name] = struct{}{}
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

func dedupeAndSort(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, n := range in {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
