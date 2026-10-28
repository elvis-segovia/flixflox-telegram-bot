// Package storage resolves the file names coming from Telegram and puts the
// files on disk without overwriting anything or leaving half-written files.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// MaxNameLen caps file and folder names (in runes, not bytes) so they stay
	// within the file system's limit.
	MaxNameLen = 120

	// maxDedupeAttempts is how many " (n)" suffixes are tried before giving up.
	maxDedupeAttempts = 1000
)

// unsafeNameChars are the characters that cannot appear in a file name: path
// separators (traversal) and the ones that break on Windows/SMB.
const unsafeNameChars = `/\:*?"<>|`

// SanitizeName turns text coming from Telegram into a safe file or folder
// name. It never returns something that could escape the destination
// directory. If nothing usable is left it returns "".
func SanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f: // control characters
			return -1
		case strings.ContainsRune(unsafeNameChars, r):
			return '_'
		default:
			return r
		}
	}, name)

	// Leading and trailing dots and spaces produce hidden names, "." or "..".
	name = strings.Trim(strings.TrimSpace(name), " .")
	if name == "" {
		return ""
	}
	return truncateName(name)
}

// truncateName trims to MaxNameLen runes, keeping the extension.
func truncateName(name string) string {
	if utf8.RuneCountInString(name) <= MaxNameLen {
		return name
	}

	ext := filepath.Ext(name)
	if utf8.RuneCountInString(ext) > 10 { // does not look like a real extension
		ext = ""
	}

	stem := []rune(strings.TrimSuffix(name, ext))
	keep := max(MaxNameLen-utf8.RuneCountInString(ext), 1)
	if len(stem) > keep {
		stem = stem[:keep]
	}

	trimmed := strings.Trim(strings.TrimRight(string(stem), " ."), " .")
	if trimmed == "" {
		trimmed = "file"
	}
	return trimmed + ext
}

// UniquePath appends " (1)", " (2)"... when the destination already exists, so
// an earlier file is never overwritten.
func UniquePath(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; i <= maxDedupeAttempts; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("too many files named %q", filepath.Base(path))
}
