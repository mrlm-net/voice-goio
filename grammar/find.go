package grammar

// Find is the intent of the command said somewhere in text: free text from
// a recogniser that transcribes (stt/whisper), where the player says the
// station and the callsign around the call — "Prague Ground, OK-ABC,
// request taxi" is request_taxi.
//
// Realism first, as a controller or a crew member would hear it:
//   - a phrase of several words matches anywhere in the text, word for word;
//     a long word (5 letters or more) may be slightly misheard ("pushbak"),
//     with its first letter right and at least one word of the phrase exact;
//   - a phrase of one word ("on", "zero", "check") only when it is all that
//     was said, so a number or a word inside a longer call is never taken
//     for a checklist answer;
//   - the longest phrase wins; two intents matched equally well are not
//     guessed (ok false).
//
// Pass text with numbers as words (whisper.Spoken), as phrases are written.
func Find(cmds []Command, text string) (intent string, ok bool) {
	said := Words(text)
	if len(said) == 0 {
		return "", false
	}
	if i, ok := Match(cmds, text); ok {
		return i, true
	}
	best, bestLen, bestExact, tie := "", 0, 0, false
	for _, c := range cmds {
		for _, p := range c.Phrases {
			words := Words(p)
			if len(words) < 2 || len(words) > len(said) {
				continue
			}
			for start := 0; start+len(words) <= len(said); start++ {
				exact, ok := phraseAt(said[start:start+len(words)], words)
				if !ok {
					continue
				}
				switch {
				case len(words) > bestLen || len(words) == bestLen && exact > bestExact:
					best, bestLen, bestExact, tie = c.Intent, len(words), exact, false
				case len(words) == bestLen && exact == bestExact && c.Intent != best:
					tie = true
				}
			}
		}
	}
	if best == "" || tie {
		return "", false
	}
	return best, true
}

// phraseAt reports whether heard is the phrase, each word exact or a long
// word near it, and how many words were exact (at least one).
func phraseAt(heard, words []string) (exact int, ok bool) {
	for i, w := range words {
		switch {
		case heard[i] == w:
			exact++
		case len(w) >= 5 && nearWord(heard[i], w):
		default:
			return 0, false
		}
	}
	return exact, exact > 0
}

// nearWord: a third of the letters may differ, never the first.
func nearWord(heard, want string) bool {
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
