package storage

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeNameBlocksTraversal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Separators become "_" and the dots at the edges are trimmed.
		{"relative path", "../../etc/passwd", "_.._etc_passwd"},
		{"absolute path", "/etc/passwd", "_etc_passwd"},
		{"backslash", `..\..\windows\system32`, "_.._windows_system32"},
		{"only dots", "..", ""},
		{"single dot", ".", ""},
		{"empty", "", ""},
		{"only spaces", "   ", ""},
		{"hidden file", ".bashrc", "bashrc"},
		{"control chars", "movie\n\t\x00.mkv", "movie.mkv"},
		{"ordinary name", "Blade Runner 2049.mkv", "Blade Runner 2049.mkv"},
		{"accents", "Amélie (2001).mkv", "Amélie (2001).mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if got == "" {
				return
			}
			// Core invariant: the result can never escape the destination.
			joined := filepath.Join("/uploads", got)
			if !strings.HasPrefix(joined, "/uploads/") {
				t.Errorf("SanitizeName(%q) escaped the directory: %q", tt.input, joined)
			}
			if strings.ContainsAny(got, `/\`) {
				t.Errorf("SanitizeName(%q) = %q keeps path separators", tt.input, got)
			}
		})
	}
}

func TestSanitizeNameTruncatesKeepingExtension(t *testing.T) {
	long := strings.Repeat("á", 300) + ".mkv"

	got := SanitizeName(long)
	if n := utf8.RuneCountInString(got); n > MaxNameLen {
		t.Errorf("name of %d runes, the maximum is %d", n, MaxNameLen)
	}
	if filepath.Ext(got) != ".mkv" {
		t.Errorf("the extension was lost: %q", got)
	}
}

func TestUniquePathAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "movie.mkv")

	got, err := UniquePath(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("with the destination free, want %q, got %q", target, got)
	}

	if err := writeAtomic(target, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}

	got, err = UniquePath(target)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "movie (1).mkv")
	if got != want {
		t.Errorf("UniquePath = %q, want %q", got, want)
	}
}
