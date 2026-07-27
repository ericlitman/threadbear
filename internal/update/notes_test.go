package update

import (
	"reflect"
	"strings"
	"testing"
)

func TestReleaseNotesPlaceholderIsEmpty(t *testing.T) {
	if notes := ReleaseNotes(); notes != nil {
		t.Fatalf("notes = %v", notes)
	}
}

func TestParseReleaseNotes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{name: "empty"},
		{name: "whitespace", content: "  \n\t"},
		{name: "bullets", content: "- first\n* second", want: []string{"first", "second"}},
		{name: "trim", content: "-   first   ", want: []string{"first"}},
		{name: "ignore prose", content: "intro\n  - nested\n- kept", want: []string{"kept"}},
		{name: "cap after validation", content: "- [bad](https://example.test)\n- first\n* second\n- third\n- fourth", want: []string{"first", "second", "third"}},
		{name: "links", content: "- [bad](https://example.test)\n- ![bad](https://example.test)\n- https://example.test\n- <https://example.test>"},
		{name: "markup", content: "- ```go\n- ~~~text\n- `inline`\n- <strong>html</strong>"},
		{name: "controls", content: "- bad\ttext\n- bad\x00text\n- \ttrimmed"},
		{name: "blank", content: "- \n*   "},
		{name: "unicode limit", content: "- " + strings.Repeat("é", 200), want: []string{strings.Repeat("é", 200)}},
		{name: "unicode oversize", content: "- " + strings.Repeat("é", 201)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseReleaseNotes(test.content); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}
