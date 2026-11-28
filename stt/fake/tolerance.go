package fake

import "strings"

// Small deviations real pilots make, and a recogniser's near misses, are
// accepted; realism comes first. A word is mapped only to the one word it can
// be, and a callsign is matched loosely only in its airline word: the flight
// number, a value and a choice between two aircraft are never guessed, so a
// doubtful transmission stays say_again, as a controller would answer it.

// alike maps a word to the phraseology word it is said or heard as.
var alike = map[string]string{
	"iden": "ident", "eiden": "ident", "idon": "ident", "idoned": "ident", "idents": "ident",
	"approve": "approved", "soles": "souls",
	"requests": "request",
	"wilko":    "wilco", "willco": "wilco",
	"squak":       "squawk",
	"affirmative": "affirm",
	"centre":      "center",
}

// alikePairs are two words said or heard for others.
var alikePairs = map[[2]string]string{
	{"stand", "by"}:           "standby",
	{"stand", "bye"}:          "standby",
	{"will", "co"}:            "wilco",
	{"i", "dent"}:             "ident",
	{"request", "descend"}:    "request descent",
	{"requesting", "descend"}: "requesting descent",
}

// tolerate rewrites the tokens to the phraseology's words.
func tolerate(toks []string) []string {
	out := toks[:0:0]
	for i := 0; i < len(toks); i++ {
		// "made a, made a, made a" is how a recogniser hears mayday; said
		// twice at least, never a single "made a" ("we made a turn").
		if n := repeats(toks[i:], "made", "a"); n >= 2 {
			for range n {
				out = append(out, "mayday")
			}
			i += 2*n - 1
			continue
		}
		if i+1 < len(toks) {
			if w, ok := alikePairs[[2]string{toks[i], toks[i+1]}]; ok {
				out = append(out, strings.Fields(w)...)
				i++
				continue
			}
		}
		// "squawk for tree two one": four, in a run of digits only.
		if toks[i] == "for" && len(out) > 0 && i+1 < len(toks) {
			_, prevDigit := digitWord[out[len(out)-1]]
			_, nextDigit := digitWord[toks[i+1]]
			if nextDigit && (prevDigit || out[len(out)-1] == "squawk") {
				out = append(out, "four")
				continue
			}
		}
		if w, ok := alike[toks[i]]; ok {
			out = append(out, w)
			continue
		}
		out = append(out, toks[i])
	}
	return out
}

// repeats counts how many times the pair starts toks over.
func repeats(toks []string, a, b string) int {
	n := 0
	for len(toks) >= 2 && toks[0] == a && toks[1] == b {
		n++
		toks = toks[2:]
	}
	return n
}

// matchCallsignLoose finds a callsign whose airline word is close to the
// one said ("lafansa four echo kilo" for Lufthansa four echo kilo) with
// every other word exact. Only when one aircraft on frequency matches.
func (p *Parser) matchCallsignLoose(toks []string) (string, []string) {
	var found string
	var at, n int
	for spoken, icao := range p.roster {
		words := strings.Fields(spoken)
		if len(words) < 2 || len(words[0]) < 4 || digitOrLetter(words[0]) {
			continue
		}
		for start := 0; start+len(words) <= len(toks); start++ {
			if !nearWord(toks[start], words[0]) || !equal(toks[start+1:start+len(words)], words[1:]) {
				continue
			}
			if found != "" && found != icao {
				return "", toks // two aircraft: not guessed
			}
			if found == "" || len(words) > n {
				found, at, n = icao, start, len(words)
			}
		}
	}
	if found == "" {
		return "", toks
	}
	rest := make([]string, 0, len(toks)-n)
	rest = append(rest, toks[:at]...)
	rest = append(rest, toks[at+n:]...)
	return found, rest
}

func digitOrLetter(w string) bool {
	_, d := digitWord[w]
	_, l := letterWord[w]
	return d || l
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nearWord reports a word heard near enough to the airline word: a third of its
// letters may differ (lufthansa/lafansa 3 of 9), but never the first letter.
func nearWord(heard, want string) bool {
	if heard == want {
		return true
	}
	if heard == "" || heard[0] != want[0] {
		return false
	}
	return editDistance(heard, want) <= len(want)/3
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
