package pipeline

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	regexCacheMu sync.RWMutex
	regexCache   = make(map[string]*regexp.Regexp)
)

// globToRegexp translates a gitignore-style glob pattern into a compiled regex.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}

	clean := filepath.ToSlash(filepath.Clean(pattern))

	var b strings.Builder
	b.WriteString("^")

	i := 0
	for i < len(clean) {
		if strings.HasPrefix(clean[i:], "/**/") {
			b.WriteString("(?:/.+/|/)")
			i += 4
		} else if strings.HasPrefix(clean[i:], "/**") {
			b.WriteString("(?:/.*)?")
			i += 3
		} else if strings.HasPrefix(clean[i:], "**/") {
			b.WriteString("(?:.*/)?")
			i += 3
		} else if clean[i] == '*' {
			b.WriteString("[^/]*")
			i++
		} else if clean[i] == '?' {
			b.WriteString("[^/]")
			i++
		} else if strings.ContainsRune(".+()|^$[]{}\\", rune(clean[i])) {
			b.WriteByte('\\')
			b.WriteByte(clean[i])
			i++
		} else {
			b.WriteByte(clean[i])
			i++
		}
	}
	b.WriteString("$")

	compiled, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	regexCache[pattern] = compiled
	regexCacheMu.Unlock()

	return compiled, nil
}

// MatchPath evaluates whether a relative file path matches a glob pattern.
func MatchPath(pattern string, targetPath string) bool {
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	targetPath = filepath.ToSlash(filepath.Clean(targetPath))

	if pattern == targetPath || pattern == "**" || pattern == "*" {
		return true
	}

	re, err := globToRegexp(pattern)
	if err != nil {
		return false
	}

	return re.MatchString(targetPath)
}

// ShouldRunForPaths determines whether a job with path filter patterns should run given changed files.
func ShouldRunForPaths(patterns []string, changedFiles []string) bool {
	if len(patterns) == 0 {
		return true // No path restrictions
	}
	if len(changedFiles) == 0 {
		return true // No change list available, run by default
	}

	var includePatterns []string
	var excludePatterns []string

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "!") {
			excludePatterns = append(excludePatterns, strings.TrimPrefix(p, "!"))
		} else if p != "" {
			includePatterns = append(includePatterns, p)
		}
	}

	for _, file := range changedFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}

		// 1. Check if explicitly excluded
		excluded := false
		for _, exp := range excludePatterns {
			if MatchPath(exp, file) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// 2. Check if matches include pattern
		if len(includePatterns) == 0 {
			return true
		}

		for _, inp := range includePatterns {
			if MatchPath(inp, file) {
				return true
			}
		}
	}

	return false
}
