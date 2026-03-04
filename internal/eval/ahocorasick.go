package eval

// ahoCorasick implements the Aho-Corasick multi-pattern string matching
// algorithm. It builds a trie from a set of keywords, then adds failure links
// to enable linear-time scanning over input text. This is an Alien Artifact
// (see plan 02-matching-framework §9): a textbook algorithm (~200 LOC)
// that matches all keywords simultaneously in O(n + m + z) time.
type ahoCorasick struct {
	states  []acState
	output  [][]int // output[stateID] → list of pattern indices
	keyword []string
}

type acState struct {
	goto_ [256]int // byte → next state (-1 = no transition)
	fail  int
	depth int
}

// newAhoCorasick builds an Aho-Corasick automaton from the given patterns.
// Patterns are matched case-sensitively.
func newAhoCorasick(patterns []string) *ahoCorasick {
	ac := &ahoCorasick{
		keyword: append([]string{}, patterns...),
	}
	if len(patterns) == 0 {
		return ac
	}

	// Root state.
	ac.addState(0)

	// Build trie (goto function).
	for pi, pat := range patterns {
		ac.enter(pat, pi)
	}

	// All root transitions that don't exist should loop back to root.
	for c := 0; c < 256; c++ {
		if ac.states[0].goto_[c] == -1 {
			ac.states[0].goto_[c] = 0
		}
	}

	// Build failure links via BFS.
	ac.buildFail()

	return ac
}

func (ac *ahoCorasick) addState(depth int) int {
	id := len(ac.states)
	var s acState
	s.depth = depth
	for i := range s.goto_ {
		s.goto_[i] = -1
	}
	ac.states = append(ac.states, s)
	ac.output = append(ac.output, nil)
	return id
}

func (ac *ahoCorasick) enter(pattern string, patternIdx int) {
	state := 0
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		next := ac.states[state].goto_[c]
		if next == -1 {
			next = ac.addState(i + 1)
			ac.states[state].goto_[c] = next
		}
		state = next
	}
	ac.output[state] = append(ac.output[state], patternIdx)
}

func (ac *ahoCorasick) buildFail() {
	// BFS queue, starting from depth-1 states.
	queue := make([]int, 0, len(ac.states))
	for c := 0; c < 256; c++ {
		s := ac.states[0].goto_[c]
		if s != 0 {
			ac.states[s].fail = 0
			queue = append(queue, s)
		}
	}

	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]

		for c := 0; c < 256; c++ {
			s := ac.states[r].goto_[c]
			if s == -1 {
				continue
			}
			queue = append(queue, s)

			// Follow failure links to find longest proper suffix.
			state := ac.states[r].fail
			for ac.states[state].goto_[c] == -1 {
				state = ac.states[state].fail
			}
			ac.states[s].fail = ac.states[state].goto_[c]

			// Merge output.
			if failOut := ac.output[ac.states[s].fail]; len(failOut) > 0 {
				ac.output[s] = append(ac.output[s], failOut...)
			}
		}
	}
}

// acMatch records where a pattern was found.
type acMatch struct {
	PatternIdx int
	Start      int
	End        int // exclusive
}

// search scans text and returns all pattern matches.
func (ac *ahoCorasick) search(text string) []acMatch {
	if len(ac.states) == 0 {
		return nil
	}

	var matches []acMatch
	state := 0

	for i := 0; i < len(text); i++ {
		c := text[i]
		for ac.states[state].goto_[c] == -1 {
			state = ac.states[state].fail
		}
		state = ac.states[state].goto_[c]

		for _, pi := range ac.output[state] {
			patLen := len(ac.keyword[pi])
			matches = append(matches, acMatch{
				PatternIdx: pi,
				Start:      i - patLen + 1,
				End:        i + 1,
			})
		}
	}

	return matches
}
