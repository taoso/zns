package zns

import (
	_ "embed"
	"slices"
	"strings"

	"github.com/dghubble/trie"
)

//go:generate sh -c "curl -s https://gcore.jsdelivr.net/gh/TG-Twilight/AWAvenue-Ads-Rule@main/Filters/AWAvenue-Ads-Rule-Mosdns_v5.txt | cut -d: -f2 > awa.txt"

//go:embed awa.txt
var dataFile string

var blacklist trie.Trier

func init() {
	blacklist = trie.NewPathTrie()
	for _, domain := range strings.Split(dataFile, "\n") {
		if domain == "" {
			continue
		}
		labels := strings.Split(domain, ".")
		slices.Reverse(labels)
		path := strings.Join(labels, "/")
		blacklist.Put(path, struct{}{})
	}
}

func isBlackDomain(name string) (black bool) {
	labels := strings.Split(name, ".")
	slices.Reverse(labels)
	path := strings.Join(labels, "/")
	blacklist.WalkPath(path, func(key string, value any) error {
		if value != nil {
			black = true
		}
		return nil
	})
	return
}
