package commands

import (
	"sort"
	"strings"
	"unicode"
)

// Match is a single fuzzy-match result with its score. Higher
// scores indicate stronger matches; the suggestion popup sorts
// descending and takes the top N for display.
type Match struct {
	Name  string
	Score int
}

// FuzzyScore computes a subsequence-match score between a needle
// and a candidate command name. The scoring favours:
//
//   - Exact prefix matches (highest)
//   - Matches at word boundaries (e.g. after '-')
//   - Earlier matches in the candidate
//   - Contiguous runs of matching characters
//
// A score of 0 means no possible match (some needle character
// isn't present in the candidate). Negative scores are never
// returned. The actual numeric scale is unimportant — only the
// relative ordering matters for ranking the dropdown.
//
// This is a deliberately simple scorer. It's good enough for ~100
// command names. If we ever need to rank thousands of entries
// (custom emotes!) we can swap in something like Smith-Waterman
// without changing the surface API.
func FuzzyScore(needle, candidate string) int {
	if needle == "" {
		return 1 // empty needle = trivial match (sort by name)
	}
	needle = strings.ToLower(needle)
	candidate = strings.ToLower(candidate)

	// Exact prefix match dominates everything else.
	if strings.HasPrefix(candidate, needle) {
		return 10000 - len(candidate) // shorter names rank higher
	}

	// Substring match is next best.
	if idx := strings.Index(candidate, needle); idx >= 0 {
		score := 5000 - idx*10 - len(candidate)
		// Word-boundary bonus.
		if idx == 0 || isBoundary(rune(candidate[idx-1])) {
			score += 200
		}
		return score
	}

	// Subsequence fallback. Walk the candidate looking for each
	// needle rune in order. Award bonuses for runs and boundaries.
	var (
		score   int
		ni      int
		needleR      = []rune(needle)
		prev    rune = -1
		runLen  int
		matched int
	)
	for _, c := range candidate {
		if ni >= len(needleR) {
			break
		}
		if c == needleR[ni] {
			matched++
			score += 10
			if isBoundary(prev) {
				score += 50
			}
			runLen++
			score += runLen * 5 // contiguous runs are valuable
			ni++
		} else {
			runLen = 0
		}
		prev = c
	}
	if ni < len(needleR) {
		return 0 // not all needle chars matched
	}
	// Penalise long candidates so the same match in a shorter
	// command name ranks higher.
	score -= (len(candidate) - matched)
	if score < 1 {
		score = 1
	}
	return score
}

func isBoundary(r rune) bool {
	if r < 0 {
		return true
	}
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '-', '_', '/', '.':
		return true
	}
	return false
}

// RankFuzzy scores every candidate against the needle and returns
// the matches in descending score order, dropping zero-score
// entries. Equal-score matches fall back to alphabetical order.
func RankFuzzy(needle string, candidates []string) []Match {
	out := make([]Match, 0, len(candidates))
	for _, c := range candidates {
		s := FuzzyScore(needle, c)
		if s <= 0 {
			continue
		}
		out = append(out, Match{Name: c, Score: s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}
