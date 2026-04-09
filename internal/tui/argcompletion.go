package tui

// Sub-argument fuzzy completion sources for the slash-command
// suggestion popup. When the user types `/cmd <space> partial`,
// the popup switches from "which command?" matches to "which
// argument?" matches based on the command's first ArgSpec.Kind.
//
// Each source is a closure capturing *Model so it can pull live
// data (friend store, channel list, theme registry, embedded
// help topics). Sources return a slice of argCandidate; the
// caller scores them against the partial typed token with the
// same FuzzyScore function used by command lookup, so ranking
// stays consistent with the rest of the popup UX.
//
// Adding a new source: drop a new argCompletionSources case
// with the appropriate ArgKind and a closure that returns the
// live candidate pool.

import (
	"sort"
	"strings"

	"github.com/rw3iss/slackers/internal/commands"
	"github.com/rw3iss/slackers/internal/theme"
)

// argCandidate is a single option for sub-argument completion.
// Name is the token inserted into the input on Tab; Description
// is the dim label rendered alongside it.
type argCandidate struct {
	Name        string
	Description string
}

// argCompletionsForKind returns the live candidate pool for the
// given ArgKind, or nil if no source is registered for that
// kind. The Model pointer gives sources access to the friend
// store, channel list, theme registry, etc. Invoked on every
// keystroke while the suggest popup is in arg-completion mode,
// so each closure must be cheap.
func (m *Model) argCompletionsForKind(kind commands.ArgKind) []argCandidate {
	switch kind {
	case commands.ArgHelpTopic:
		topics := commands.Topics()
		out := make([]argCandidate, 0, len(topics))
		for _, t := range topics {
			out = append(out, argCandidate{
				Name:        t,
				Description: "help topic",
			})
		}
		return out

	case commands.ArgThemeName:
		all := theme.LoadAll()
		out := make([]argCandidate, 0, len(all))
		for _, t := range all {
			desc := "dark theme"
			if !t.IsDark() {
				desc = "light theme"
			}
			out = append(out, argCandidate{
				Name:        t.Name,
				Description: desc,
			})
		}
		return out

	case commands.ArgFriendID:
		if m.friendStore == nil {
			return nil
		}
		all := m.friendStore.All()
		out := make([]argCandidate, 0, len(all))
		for _, f := range all {
			label := f.Name
			if label == "" {
				label = f.Email
			}
			if label == "" {
				label = f.UserID
			}
			status := "offline"
			if f.Online {
				status = "online"
			}
			out = append(out, argCandidate{
				Name:        label,
				Description: "friend · " + status,
			})
		}
		return out

	case commands.ArgChannelID:
		chans := m.channels.AllChannels()
		out := make([]argCandidate, 0, len(chans))
		for _, ch := range chans {
			prefix := "#"
			switch {
			case ch.IsFriend:
				prefix = "👤"
			case ch.IsDM:
				prefix = "@"
			case ch.IsPrivate, ch.IsGroup:
				prefix = "🔒"
			}
			out = append(out, argCandidate{
				Name:        ch.Name,
				Description: prefix + " channel",
			})
		}
		return out
	}
	return nil
}

// rankArgCandidates fuzzy-scores the candidate pool against the
// partial token the user has typed and returns the top N matches.
// When partial is empty, returns the first N candidates in their
// original order so the popup still shows something useful.
//
// Names containing whitespace (e.g. a friend named "Ryan Weiss")
// would break the shell-style tokenization on Tab-complete, so
// we quote them before rendering the Name field. The description
// stays unquoted — it's only visual.
func rankArgCandidates(partial string, candidates []argCandidate, topN int) []argCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if partial == "" {
		out := make([]argCandidate, 0, topN)
		for i, c := range candidates {
			if i >= topN {
				break
			}
			out = append(out, c)
		}
		return out
	}
	type scored struct {
		c     argCandidate
		score int
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		s := commands.FuzzyScore(partial, c.Name)
		if s <= 0 {
			// Fall back to substring match on the description
			// so typing "light" surfaces all light themes.
			if strings.Contains(strings.ToLower(c.Description), strings.ToLower(partial)) {
				s = 1
			}
		}
		if s > 0 {
			scoredList = append(scoredList, scored{c, s})
		}
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].score != scoredList[j].score {
			return scoredList[i].score > scoredList[j].score
		}
		return scoredList[i].c.Name < scoredList[j].c.Name
	})
	if topN > 0 && len(scoredList) > topN {
		scoredList = scoredList[:topN]
	}
	out := make([]argCandidate, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.c
	}
	return out
}

// quoteArgIfNeeded wraps a candidate name in double quotes if it
// contains whitespace, so Tab-completing into the input bar
// produces a token that the command runner's shell-style
// tokenizer will treat as a single argument.
func quoteArgIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}
