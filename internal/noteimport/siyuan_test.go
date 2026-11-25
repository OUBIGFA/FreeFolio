package noteimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiYuanCodeInfoColumnsCalloutAndStaticEmbed(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put(t, src, "book/n.sy", `{"ID":"root","Type":"NodeDocument","Properties":{"title":"Structures"},"Children":[{"Type":"NodeCodeBlock","CodeBlockInfo":"Z28=","Children":[{"Type":"NodeCodeBlockCode","Data":"fmt.Println(1)"}]},{"Type":"NodeCallout","CalloutType":"warning","CalloutTitle":"Watch this","Children":[{"Type":"NodeParagraph","Children":[{"Type":"NodeText","Data":"body"}]}]},{"Type":"NodeSuperBlock","Children":[{"Type":"NodeSuperBlockLayoutMarker","Data":"col"},{"Type":"NodeParagraph","Children":[{"Type":"NodeText","Data":"left"}]},{"Type":"NodeParagraph","Children":[{"Type":"NodeText","Data":"right"}]}]},{"Type":"NodeBlockQueryEmbed","Children":[{"Type":"NodeBlockQueryEmbedScript","Data":"SELECT * FROM blocks WHERE id = '20240101000000-abcdefg'"}]},{"ID":"20240101000000-abcdefg","Type":"NodeParagraph","Children":[{"Type":"NodeText","Data":"embedded content"}]}]}`)
	r, e := Import(src, dst)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dst, r.Folder, "book/Structures.md"))
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"```go", "[!WARNING] Watch this", "freefolio-columns"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q: %s", want, b)
		}
	}
	if strings.Count(string(b), "embedded content") != 2 {
		t.Fatalf("static embed not copied: %s", b)
	}
}
