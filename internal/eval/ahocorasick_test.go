package eval

import (
	"sort"
	"testing"
)

func TestAhoCorasick_BasicMatch(t *testing.T) {
	ac := newAhoCorasick([]string{"git", "rm", "push"})

	matches := ac.search("git push --force")
	if len(matches) == 0 {
		t.Fatal("expected matches")
	}

	keywords := matchedKeywords(ac, matches)
	sort.Strings(keywords)
	want := []string{"git", "push"}
	if len(keywords) != len(want) {
		t.Fatalf("got %v, want %v", keywords, want)
	}
	for i := range want {
		if keywords[i] != want[i] {
			t.Errorf("keyword[%d] = %q, want %q", i, keywords[i], want[i])
		}
	}
}

func TestAhoCorasick_NoMatch(t *testing.T) {
	ac := newAhoCorasick([]string{"git", "rm"})
	matches := ac.search("ls -la")
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %d", len(matches))
	}
}

func TestAhoCorasick_OverlappingPatterns(t *testing.T) {
	ac := newAhoCorasick([]string{"he", "she", "his", "hers"})
	matches := ac.search("ushers")
	keywords := matchedKeywords(ac, matches)

	// "she", "he", "hers" should all be found in "ushers"
	kwSet := make(map[string]bool)
	for _, kw := range keywords {
		kwSet[kw] = true
	}
	for _, want := range []string{"she", "he", "hers"} {
		if !kwSet[want] {
			t.Errorf("expected %q in matches, got %v", want, keywords)
		}
	}
}

func TestAhoCorasick_EmptyPatterns(t *testing.T) {
	ac := newAhoCorasick(nil)
	matches := ac.search("anything")
	if len(matches) != 0 {
		t.Errorf("expected no matches from empty automaton")
	}
}

func TestAhoCorasick_EmptyText(t *testing.T) {
	ac := newAhoCorasick([]string{"git"})
	matches := ac.search("")
	if len(matches) != 0 {
		t.Errorf("expected no matches on empty text")
	}
}

func TestAhoCorasick_MatchPositions(t *testing.T) {
	ac := newAhoCorasick([]string{"rm"})
	matches := ac.search("rm -rf /")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Start != 0 || matches[0].End != 2 {
		t.Errorf("position = [%d, %d), want [0, 2)", matches[0].Start, matches[0].End)
	}
}

func TestAhoCorasick_MultipleOccurrences(t *testing.T) {
	ac := newAhoCorasick([]string{"ab"})
	matches := ac.search("ababab")
	if len(matches) != 3 {
		t.Errorf("expected 3 matches, got %d", len(matches))
	}
}

func TestAhoCorasick_CaseSensitive(t *testing.T) {
	ac := newAhoCorasick([]string{"git"})
	matches := ac.search("GIT push")
	if len(matches) != 0 {
		t.Errorf("expected no matches (case-sensitive), got %d", len(matches))
	}
}

func matchedKeywords(ac *ahoCorasick, matches []acMatch) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, m := range matches {
		kw := ac.keyword[m.PatternIdx]
		if _, ok := seen[kw]; !ok {
			seen[kw] = struct{}{}
			result = append(result, kw)
		}
	}
	return result
}
