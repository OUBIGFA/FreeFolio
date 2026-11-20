package wiki

import "testing"

func TestHTMLCommentsDoNotCreateLinksOrTags(t *testing.T) {
	text := "[[Visible]] #shown <!-- [[Hidden]] #secret -->\n<!--\n[[Also hidden]] #hidden\n-->"
	links := Links(text)
	if len(links) != 1 || links[0].Target != "Visible" {
		t.Fatalf("comment links leaked: %+v", links)
	}
	tags := Tags(text)
	if len(tags) != 1 || tags[0].Name != "shown" {
		t.Fatalf("comment tags leaked: %+v", tags)
	}
}
