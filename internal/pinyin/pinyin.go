// Package pinyin provides pure-Go Mandarin pinyin lookup and fuzzy/prefix
// token matching for Hanzi text without external dependencies or UI imports.
package pinyin

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// polyphones lists common multi-reading characters whose secondary Mandarin
// readings appear in document titles, dates, and folder names.
var polyphones = map[rune][]string{
	'重': {"chong"},
	'行': {"hang"},
	'长': {"chang"},
	'调': {"tiao"},
	'乐': {"yue"},
	'藏': {"zang"},
	'传': {"zhuan"},
	'单': {"shan"},
	'解': {"xie"},
	'朝': {"zhao"},
	'厦': {"xia"},
	'率': {"shuai"},
	'省': {"xing"},
	'参': {"shen"},
	'差': {"chai", "ci"},
	'还': {"huan"},
	'觉': {"jiao"},
	'校': {"jiao"},
	'便': {"pian"},
	'曾': {"zeng"},
	'着': {"zhuo", "zhao"},
	'宿': {"xiu"},
	'称': {"chen"},
	'折': {"she"},
	'弹': {"tan"},
	'奇': {"ji"},
	'盛': {"cheng"},
	'嗯': {"en"},
	'地': {"di"},
	'得': {"dei"},
	'给': {"ji"},
	'和': {"huo"},
	'会': {"kuai"},
	'数': {"shuo"},
	'泊': {"bo"},
	'角': {"jue"},
	'降': {"xiang"},
	'查': {"zha"},
	'卡': {"qia"},
	'都': {"du"},
	'说': {"shui"},
	'种': {"chong"},
	'系': {"ji"},
	'秘': {"bi"},
	'否': {"pi"},
	'模': {"mu"},
	'度': {"duo"},
	'读': {"dou"},
	'区': {"ou"},
	'仇': {"qiu"},
	'朴': {"piao"},
	'繁': {"po"},
	'拾': {"she"},
	'万': {"mo"},
	'句': {"gou"},
	'石': {"dan"},
	'伯': {"bai"},
	'殷': {"yan"},
	'盖': {"ge"},
}

// Syllables returns the Mandarin pinyin readings (toneless, lowercase) for a
// Hanzi rune, including common polyphones and v/u variants for ü syllables
// (e.g. "lv" and "lu"), or nil if r has no entry.
func Syllables(r rune) []string {
	var buf [8]string
	n := fillSyllables(r, &buf)
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	copy(out, buf[:n])
	return out
}

func primarySyllable(r rune) string {
	if r == '〇' {
		return "ling"
	}
	if r < hanBase || r > hanEnd {
		return ""
	}
	idx := int(r - hanBase)
	bitPos := idx * 9
	bytePos := bitPos >> 3
	shift := bitPos & 7
	id := (int(primaryData[bytePos]) | int(primaryData[bytePos+1])<<8) >> shift & 0x1FF
	if id <= 0 || id+1 >= len(syllableOffsets) {
		return ""
	}
	return syllableBlob[syllableOffsets[id]:syllableOffsets[id+1]]
}

func fillSyllables(r rune, buf *[8]string) int {
	p := primarySyllable(r)
	if p == "" {
		return 0
	}
	n := 0
	add := func(s string) {
		for i := 0; i < n; i++ {
			if buf[i] == s {
				return
			}
		}
		if n < len(buf) {
			buf[n] = s
			n++
		}
	}
	addWithDiaeresis := func(s string) {
		add(s)
		if strings.ContainsRune(s, 'v') {
			add(strings.ReplaceAll(s, "v", "u"))
		}
	}
	addWithDiaeresis(p)
	if extra, ok := polyphones[r]; ok {
		for _, s := range extra {
			addWithDiaeresis(s)
		}
	}
	return n
}

// HasHanzi reports whether s contains any rune with pinyin data.
func HasHanzi(s string) bool {
	for _, r := range s {
		if r == '〇' || (r >= hanBase && r <= hanEnd) {
			return true
		}
	}
	return false
}

// ContainsToken reports whether token matches anywhere in haystack as a literal
// substring (case-insensitive) or via pinyin syllables/initials.
func ContainsToken(haystack, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return true
	}
	if strings.Contains(strings.ToLower(haystack), token) {
		return true
	}
	_, ok := ScoreToken(token, haystack)
	return ok
}

// ScoreToken matches a single trimmed query token against target using both
// case-insensitive literal runes and Mandarin pinyin syllables/initials.
// A Hanzi rune can match its full syllable, a leading initial, or (at the end
// of token) a syllable prefix; arbitrary mid-syllable substrings never match.
func ScoreToken(token, target string) (int, bool) {
	q := strings.ToLower(strings.TrimSpace(token))
	if q == "" {
		return 0, true
	}
	if len(q) > 96 {
		return 0, false
	}
	if target == "" {
		return 0, false
	}

	// The table has a row for each rune of target, and a row reads only the
	// one before it: two rows are kept, on the stack, where the whole table
	// was allocated for every title a query was scored against.
	//
	// dp0: best score when previous target rune was NOT matched into current run.
	// dp1: best score when previous target rune WAS matched right before ti.
	// Using -1e9 as unreachable sentinel.
	const negInf = -1_000_000_000
	qn := len(q)
	var rows [4][97]int
	dp0, dp1, next0, next1 := rows[0][:qn+1], rows[1][:qn+1], rows[2][:qn+1], rows[3][:qn+1]
	for i := range dp0 {
		dp0[i], dp1[i] = negInf, negInf
	}
	dp0[0] = 0

	var sylBuf [8]string
	ti := 0
	prev := ' '
	for _, r := range target {
		if ti == 512 {
			break // a longer target is scored by its first 512 runes
		}
		tr := unicode.ToLower(r)
		ns := fillSyllables(r, &sylBuf)
		boundBonus := 0
		if ti == 0 {
			boundBonus = 8
		} else if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
			boundBonus = 5 // a word boundary
		}
		prev = r
		ti++

		for i := range next0 {
			next0[i], next1[i] = negInf, negInf
		}
		for qi := 0; qi <= qn; qi++ {
			s0 := dp0[qi]
			s1 := dp1[qi]
			bestHere := max(s0, s1)
			if bestHere <= negInf/2 {
				continue
			}
			// Skip target rune ti without consuming query bytes.
			if bestHere > next0[qi] {
				next0[qi] = bestHere
			}
			if qi == qn {
				continue
			}

			// Transition 1: literal rune match.
			qr, qSize := utf8.DecodeRuneInString(q[qi:])
			if tr == qr {
				val0 := negInf
				if s0 > negInf/2 {
					val0 = s0 + 14 + boundBonus
				}
				val1 := negInf
				if s1 > negInf/2 {
					val1 = s1 + 14 + 6 + boundBonus
				}
				cand := max(val0, val1)
				if cand > next1[qi+qSize] {
					next1[qi+qSize] = cand
				}
			}

			// Transition 2: Hanzi pinyin match when q[qi] is ASCII a..z.
			if ns > 0 && q[qi] >= 'a' && q[qi] <= 'z' {
				rem := q[qi:]
				for si := 0; si < ns; si++ {
					syl := sylBuf[si]
					// Polyphone secondary readings take a tiny penalty vs primary so
					// primary readings break ties cleanly.
					polyPenalty := 0
					if si > 0 {
						polyPenalty = 1
					}

					// 2a: Full syllable match.
					if strings.HasPrefix(rem, syl) {
						step := 12 + boundBonus - polyPenalty
						cand := max(s0+step, s1+step+6)
						nextQ := qi + len(syl)
						if cand > next1[nextQ] {
							next1[nextQ] = cand
						}
					}

					// 2b: Trailing syllable prefix at the very end of the query
					// (e.g. "zhoub" -> "周报" after "zhou" matches '周', or "zho" -> "周").
					if len(rem) >= 2 && len(rem) < len(syl) && strings.HasPrefix(syl, rem) {
						step := 8 + len(rem) + boundBonus - polyPenalty
						cand := max(s0+step, s1+step+6)
						if cand > next1[qn] {
							next1[qn] = cand
						}
					}

					// 2c: Initial letter match.
					if rem[0] == syl[0] {
						step := 4 + boundBonus - polyPenalty
						cand := max(s0+step, s1+step+4)
						if cand > next1[qi+1] {
							next1[qi+1] = cand
						}
					}

					// 2d: Two-letter consonant digraph initial ("zh", "ch", "sh")
					// when there are still more query bytes after it.
					if len(rem) > 2 && len(syl) >= 2 && (syl[:2] == "zh" || syl[:2] == "ch" || syl[:2] == "sh") && rem[:2] == syl[:2] {
						step := 6 + boundBonus - polyPenalty
						cand := max(s0+step, s1+step+4)
						if cand > next1[qi+2] {
							next1[qi+2] = cand
						}
					}
				}
			}
		}
		dp0, next0 = next0, dp0
		dp1, next1 = next1, dp1
	}

	best := max(dp0[qn], dp1[qn])
	if best <= negInf/2 {
		return 0, false
	}
	return best*100 - len(target), true
}
