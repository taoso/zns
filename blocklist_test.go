package zns

import (
	"strings"
	"testing"
)

func TestBlockList(t *testing.T) {
	rule := `
# demo title

# demo comment

full:a.example.com # trailing comment
domain:example.net
keyword:-ad.example.org
`

	b := NewBlockList()
	b.ParseMosdnsV5(strings.NewReader(rule))

	for _, test := range []struct {
		domain   string
		expected bool
	}{
		{"a.example.com", true},        // full match
		{"b.example.com", false},       // no match
		{"sub.example.net", true},      // domain match
		{"example.net", false},         // no match
		{"video-ad.example.org", true}, // keyword match
		{"example.org", false},         // no match
	} {
		result := b.Exists(test.domain)
		if result != test.expected {
			t.Errorf("Has(%q) = %v; want %v", test.domain, result, test.expected)
		}
	}
}
