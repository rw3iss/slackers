package commands

import "testing"

func TestTriePrefixLookup(t *testing.T) {
	tr := newTrie()
	for _, name := range []string{"theme", "themes", "them", "version", "vibrate"} {
		tr.Insert(name)
	}
	got := tr.Lookup("the")
	want := []string{"them", "theme", "themes"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v) want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d: got %q want %q", i, got[i], want[i])
		}
	}
	// Empty prefix returns everything.
	if all := tr.Lookup(""); len(all) != 5 {
		t.Errorf("empty lookup: got %d, want 5: %v", len(all), all)
	}
	// Non-existent prefix returns nothing.
	if none := tr.Lookup("zz"); len(none) != 0 {
		t.Errorf("zz lookup: got %v want empty", none)
	}
}

func TestFuzzyScoreOrdering(t *testing.T) {
	cases := []struct {
		needle string
		better string
		worse  string
	}{
		{"the", "theme", "thrombolytic"},    // shorter prefix wins
		{"add", "add-friend", "padding"},    // prefix beats substring
		{"addfri", "add-friend", "address"}, // subsequence with run wins
		{"hf", "help-friend", "shaft"},      // boundary bonus
	}
	for _, tc := range cases {
		b := FuzzyScore(tc.needle, tc.better)
		w := FuzzyScore(tc.needle, tc.worse)
		if b <= w {
			t.Errorf("needle=%q: %q (%d) should beat %q (%d)",
				tc.needle, tc.better, b, tc.worse, w)
		}
	}
}

func TestFuzzyScoreNoMatch(t *testing.T) {
	if s := FuzzyScore("xyz", "command"); s != 0 {
		t.Errorf("expected 0 score, got %d", s)
	}
}

func TestRegistryRunUnknown(t *testing.T) {
	r := NewRegistry()
	res := r.Run("/nope")
	if res.Status != StatusError {
		t.Errorf("expected error status, got %v", res.Status)
	}
}

func TestRegistryRunWithArgs(t *testing.T) {
	r := NewRegistry()
	var got *Context
	if err := r.Register(Command{
		Name: "echo",
		Run: func(ctx *Context) Result {
			got = ctx
			return Result{Status: StatusOK, StatusBar: "ok"}
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	res := r.Run(`/echo hello "two words" three`)
	if res.Status != StatusOK {
		t.Fatalf("expected ok, got %v: %q", res.Status, res.StatusBar)
	}
	if got == nil {
		t.Fatal("Run did not invoke handler")
	}
	want := []string{"hello", "two words", "three"}
	if len(got.Args) != len(want) {
		t.Fatalf("args: got %v want %v", got.Args, want)
	}
	for i := range want {
		if got.Args[i] != want[i] {
			t.Errorf("arg %d: got %q want %q", i, got.Args[i], want[i])
		}
	}
}

func TestRegistryLookupPrefix(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"add-friend", "remove-friend", "friends", "channels", "version"} {
		_ = r.Register(Command{Name: name, Run: func(*Context) Result { return Result{} }})
	}
	got := r.Lookup("/fri", 5)
	if len(got) == 0 || got[0].Name != "friends" {
		t.Errorf("/fri should rank friends first; got %v", names(got))
	}
	if all := r.Lookup("", 100); len(all) != 5 {
		t.Errorf("empty query: got %d want 5", len(all))
	}
}

func TestRegistryAliasResolution(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Command{Name: "quit", Aliases: []string{"q", "exit"}})
	for _, q := range []string{"/quit", "/q", "/exit", "QUIT", "Q"} {
		if c := r.Get(q); c == nil || c.Name != "quit" {
			t.Errorf("Get(%q) failed", q)
		}
	}
}

func names(cmds []*Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
