package theme

import "testing"

// TestNewLightThemes verifies the four new light themes load from
// the embedded builtinFS, self-identify as light mode, surface the
// "(light)" suffix in DisplayName, and parse a non-empty background
// color out of the bg slot of the background key.
//
// This is a regression guard against accidental drops of any of
// the four new built-ins or a typo in the background "/<color>"
// syntax that would silently leave the pane bg empty.
func TestNewLightThemes(t *testing.T) {
	all := LoadAll()
	byName := make(map[string]Theme, len(all))
	for _, th := range all {
		byName[th.Name] = th
	}
	wants := []string{"Aurora", "Mint Cream", "Soft Sun", "Sakura"}
	for _, name := range wants {
		th, ok := byName[name]
		if !ok {
			t.Errorf("missing built-in light theme %q", name)
			continue
		}
		if th.Mode != "light" {
			t.Errorf("%s: mode=%q want light", name, th.Mode)
		}
		if got := th.DisplayName(); got != name+" (light)" {
			t.Errorf("%s: DisplayName=%q want %q", name, got, name+" (light)")
		}
		_, bg := ParseColor(th.Get(KeyBackground))
		if bg == "" {
			t.Errorf("%s: background bg slot is empty (use \"/<color>\" syntax)", name)
		}
	}
}

func TestDisplayNameDarkThemeUntagged(t *testing.T) {
	d := Default()
	if got := d.DisplayName(); got != "Default" {
		t.Errorf("Default.DisplayName()=%q want %q", got, "Default")
	}
}
