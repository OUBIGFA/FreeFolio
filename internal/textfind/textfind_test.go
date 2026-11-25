package textfind

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

func ranges(m *Matcher, text string) [][2]int {
	var out [][2]int
	m.Each(text, func(a, z int) { out = append(out, [2]int{a, z}) })
	return out
}

func TestLiteralIgnoresCaseByDefault(t *testing.T) {
	m := New("go", Options{})
	if got := fmt.Sprint(ranges(m, "Go go GO 狗")); got != "[[0 2] [3 5] [6 8]]" {
		t.Fatal(got)
	}
	if s, n := m.Replace("Go go GO 狗", "Rust"); s != "Rust Rust Rust 狗" || n != 3 {
		t.Fatal(s, n)
	}
	if s, n := m.Replace("abc", "y"); s != "abc" || n != 0 {
		t.Fatal(s, n)
	}
}

func TestCaseSensitive(t *testing.T) {
	m := New("Go", Options{CaseSensitive: true})
	if s, n := m.Replace("Go go GO", "X"); s != "X go GO" || n != 1 {
		t.Fatal(s, n)
	}
}

func TestWholeWord(t *testing.T) {
	m := New("cat", Options{WholeWord: true})
	if s, n := m.Replace("cat concat cat_x cats (cat) Cat", "dog"); s != "dog concat cat_x cats (dog) dog" || n != 3 {
		t.Fatal(s, n)
	}
	// A rejected match does not hide a later one that overlaps it.
	if got := fmt.Sprint(ranges(New("aa", Options{WholeWord: true}), "aaa aa")); got != "[[4 6]]" {
		t.Fatal(got)
	}
	// Chinese has no spaces between words: its characters match anywhere.
	if s, n := New("名字", Options{WholeWord: true}).Replace("旧名字和名字", "称呼"); s != "旧称呼和称呼" || n != 2 {
		t.Fatal(s, n)
	}
	// A query that starts or ends with punctuation needs no word edge there.
	if n := New("-x", Options{WholeWord: true}).Count("a-x b"); n != 1 {
		t.Fatal(n)
	}
}

func TestRegex(t *testing.T) {
	m := New(`(\d+)-(\d+)`, Options{Regex: true})
	if s, n := m.Replace("1-2 and 30-40", "$2-$1"); s != "2-1 and 40-30" || n != 2 {
		t.Fatal(s, n)
	}
	if got := m.Expand("1-2 and 30-40", 8, "${2}:${1}"); got != "40:30" {
		t.Fatal(got)
	}
	if got := New(`^item`, Options{Regex: true}).Count("item\nItem\n item"); got != 2 {
		t.Fatal("lines and case", got)
	}
	if got := New(`^item`, Options{Regex: true, CaseSensitive: true}).Count("item\nItem"); got != 1 {
		t.Fatal(got)
	}
	// Empty matches are never matches.
	if got := New(`x*`, Options{Regex: true}).Count("abc"); got != 0 {
		t.Fatal(got)
	}
	if m := New(`a(`, Options{Regex: true}); m.Err() == nil || m.Count("a(") != 0 {
		t.Fatal("invalid regex")
	}
	if got := New(`\w+`, Options{Regex: true, WholeWord: true}).Count("ab cd"); got != 2 {
		t.Fatal(got)
	}
}

// reference is the plain quadratic search the matcher must agree with.
func reference(text, query string) [][2]int {
	fold := unicode.ToLower
	q := []rune(strings.Map(fold, query))
	if len(q) == 0 {
		return nil
	}
	runes := []rune(text)
	offsets := make([]int, len(runes)+1)
	for i, r := range runes {
		offsets[i+1] = offsets[i] + len(string(r))
	}
	var out [][2]int
	for i := 0; i+len(q) <= len(runes); {
		ok := true
		for j, r := range q {
			if fold(runes[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{offsets[i], offsets[i+len(q)]})
			i += len(q)
			continue
		}
		i++
	}
	return out
}

func TestLiteralMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []rune("aAbB中😀KkİıΣσς\ufffd")
	for range 800 {
		text := make([]rune, rng.Intn(150))
		for i := range text {
			text[i] = alphabet[rng.Intn(len(alphabet))]
		}
		query := make([]rune, rng.Intn(8))
		for i := range query {
			query[i] = alphabet[rng.Intn(len(alphabet))]
		}
		got, want := ranges(New(string(query), Options{}), string(text)), reference(string(text), string(query))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q / %q: %v vs %v", string(text), string(query), got, want)
		}
	}
}
