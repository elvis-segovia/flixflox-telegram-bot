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
		// Los separadores pasan a "_" y los puntos del borde se recortan.
		{"ruta relativa", "../../etc/passwd", "_.._etc_passwd"},
		{"ruta absoluta", "/etc/passwd", "_etc_passwd"},
		{"backslash", `..\..\windows\system32`, "_.._windows_system32"},
		{"solo puntos", "..", ""},
		{"punto", ".", ""},
		{"vacío", "", ""},
		{"solo espacios", "   ", ""},
		{"archivo oculto", ".bashrc", "bashrc"},
		{"control chars", "pelicula\n\t\x00.mkv", "pelicula.mkv"},
		{"nombre normal", "Blade Runner 2049.mkv", "Blade Runner 2049.mkv"},
		{"acentos", "Amélie (2001).mkv", "Amélie (2001).mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeName(%q) = %q, se esperaba %q", tt.input, got, tt.want)
			}
			if got == "" {
				return
			}
			// Invariante central: el resultado nunca puede escapar del destino.
			joined := filepath.Join("/uploads", got)
			if !strings.HasPrefix(joined, "/uploads/") {
				t.Errorf("SanitizeName(%q) escapó del directorio: %q", tt.input, joined)
			}
			if strings.ContainsAny(got, `/\`) {
				t.Errorf("SanitizeName(%q) = %q conserva separadores de ruta", tt.input, got)
			}
		})
	}
}

func TestSanitizeNameTruncatesKeepingExtension(t *testing.T) {
	long := strings.Repeat("á", 300) + ".mkv"

	got := SanitizeName(long)
	if n := utf8.RuneCountInString(got); n > MaxNameLen {
		t.Errorf("nombre de %d runas, el máximo es %d", n, MaxNameLen)
	}
	if filepath.Ext(got) != ".mkv" {
		t.Errorf("se perdió la extensión: %q", got)
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
		t.Fatalf("con el destino libre se esperaba %q, se obtuvo %q", target, got)
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
		t.Errorf("UniquePath = %q, se esperaba %q", got, want)
	}
}
