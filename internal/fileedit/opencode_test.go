package fileedit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// These goldens were recorded by executing the pinned, unmodified upstream
// algorithms against OpenCode's patch.test.ts. No network/JS runtime in tests.
func TestOpenCodePatchCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode-patch.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Kind, Error string
		Args              []json.RawMessage
		Result            json.RawMessage
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			var path string
			json.Unmarshal(tt.Args[0], &path)
			if tt.Kind == "parse" {
				got, err := Parse(path)
				if tt.Error != "" {
					if err == nil || err.Error() != tt.Error {
						t.Fatalf("error %v; want %s", err, tt.Error)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want []Hunk
				json.Unmarshal(tt.Result, &want)
				if len(got) != len(want) {
					t.Fatalf("hunks: %+v want %+v", got, want)
				}
				for i := range got {
					for j := range got[i].Chunks {
						if len(got[i].Chunks[j].OldLines) == 0 {
							got[i].Chunks[j].OldLines = nil
						}
						if len(got[i].Chunks[j].NewLines) == 0 {
							got[i].Chunks[j].NewLines = nil
						}
					}
					for j := range want[i].Chunks {
						if len(want[i].Chunks[j].OldLines) == 0 {
							want[i].Chunks[j].OldLines = nil
						}
						if len(want[i].Chunks[j].NewLines) == 0 {
							want[i].Chunks[j].NewLines = nil
						}
					}
					if !reflect.DeepEqual(got[i], want[i]) {
						t.Fatalf("hunk %+v; want %+v", got[i], want[i])
					}
				}
			} else {
				var chunks []Chunk
				var original string
				json.Unmarshal(tt.Args[1], &chunks)
				json.Unmarshal(tt.Args[2], &original)
				got, err := Derive(path, chunks, original)
				if tt.Error != "" {
					if err == nil || err.Error() != tt.Error {
						t.Fatalf("error %v; want %s", err, tt.Error)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want struct {
					Content string
					BOM     bool
				}
				json.Unmarshal(tt.Result, &want)
				if want.BOM {
					want.Content = "\ufeff" + want.Content
				}
				if got != want.Content {
					t.Fatalf("result %q; want %q", got, want.Content)
				}
			}
		})
	}
}

func TestOpenCodeEditCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode-edit.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, OldString, NewString, Result string
		ReplaceAll, Error                          bool
		Count                                      int
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			got, count, err := Edit(tt.Source, tt.OldString, tt.NewString, tt.ReplaceAll)
			if tt.Error {
				if err == nil {
					t.Fatal("accepted invalid edit")
				}
				return
			}
			if err != nil || got != tt.Result || count != tt.Count {
				t.Fatalf("got %q %d %v; want %q %d", got, count, err, tt.Result, tt.Count)
			}
		})
	}
}
