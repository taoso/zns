package zns

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// BlockList type
// Supports wildcard domains like "*.example.com" to block all subdomains.
// Supports suffix wildcards like "*-ad.example.com" to block domains ending with "-ad.example.com".
type BlockList struct {
	m      map[string]bool // blocked domains (exact matches)
	wild   map[string]bool // wildcard domains suffix -> true (e.g., "example.com." from "*.example.com.")
	suffix []string        // domains that are suffix blocked (e.g., "video-ad.example.com" from "*-ad.example.com")
}

// NewBlockList returns a new BlockList.
func NewBlockList() *BlockList {
	b := &BlockList{
		m:      map[string]bool{},
		wild:   map[string]bool{},
		suffix: []string{},
	}

	return b
}

// (*BlockList).Exists exists returns whether or not a key exists in the cache.
func (b *BlockList) Exists(key string) bool {
	if b == nil {
		return false
	}

	// Direct match - fastest path
	if b.m[key] {
		return true
	}

	// No wildcards to check
	if len(b.wild) == 0 {
		return false
	}

	// Check wildcard matches by walking up the domain hierarchy
	// For "sub.example.com." check "example.com." and "com."
	offset := 0
	for {
		idx := strings.IndexByte(key[offset:], '.')
		if idx == -1 {
			break
		}
		offset += idx + 1 // Move past the dot

		if offset < len(key) {
			suffix := key[offset:]
			if _, ok := b.wild[suffix]; ok {
				return true
			}
		}
	}

	if len(b.suffix) == 0 {
		return false
	}

	// Check suffix matches
	// For "video-ad.example.com" check against all suffixes in "*-ad.example.com"
	for _, s := range b.suffix {
		if strings.HasSuffix(key, s) {
			return true
		}
	}

	return false
}

func (b *BlockList) ParseMosdnsV5(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle inline comments using strings.Cut
		if domain, _, found := strings.Cut(line, "#"); found {
			line = strings.TrimSpace(domain)
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		pattern := strings.TrimSpace(parts[1])
		switch key {
		case "full":
			b.m[pattern] = true
		case "domain":
			b.wild[pattern] = true
		case "keyword":
			b.suffix = append(b.suffix, pattern)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error scanning hostfile: %w", err)
	}

	return nil
}

func (b *BlockList) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return b.ParseMosdnsV5(f)
}
