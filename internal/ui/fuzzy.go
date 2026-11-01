package ui

import (
	"strings"
	"unicode"
)

// fuzzy scores text against a query: every whitespace-separated token must match, either as a substring
// (scores 70–130, best near the start and on word boundaries) or as a subsequence (scores ≤ 60).
// Higher is better; ok=false when any token does not match.
func fuzzy(q, text string) (score int, ok bool) {
	text = strings.ToLower(text)
	toks := strings.Fields(strings.ToLower(q))
	if len(toks) == 0 {
		return 0, true
	}
	for _, t := range toks {
		s, m := fuzzyTok(t, text)
		if !m {
			return 0, false
		}
		score += s
	}
	return score, true
}

func fuzzyTok(tok, text string) (int, bool) {
	if i := strings.Index(text, tok); i >= 0 {
		s := 100 - min(i, 30)
		if i == 0 || !isWordRune(rune(text[i-1])) {
			s += 30
		}
		return s, true
	}
	rt, rq := []rune(text), []rune(tok)
	qi, s, last := 0, 0, -2
	for ti, r := range rt {
		if qi < len(rq) && r == rq[qi] {
			g := 4
			if ti == last+1 {
				g += 6 // consecutive
			}
			if ti == 0 || !isWordRune(rt[ti-1]) {
				g += 8 // word start
			}
			s += g
			last = ti
			qi++
		}
	}
	if qi < len(rq) {
		return 0, false
	}
	return min(s, 60), true
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
