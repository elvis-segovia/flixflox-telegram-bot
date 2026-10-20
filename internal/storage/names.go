// Package storage resuelve los nombres de archivo que vienen de Telegram y
// deja los archivos en disco sin pisar nada ni escribir a medias.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// MaxNameLen limita nombres de archivo y carpeta (en runas, no en bytes)
	// para no pasarse del límite del sistema de archivos.
	MaxNameLen = 120

	// maxDedupeAttempts es cuántos sufijos " (n)" se prueban antes de rendirse.
	maxDedupeAttempts = 1000
)

// unsafeNameChars son los caracteres que no pueden aparecer en un nombre de
// archivo: separadores de ruta (traversal) y los que rompen en Windows/SMB.
const unsafeNameChars = `/\:*?"<>|`

// SanitizeName convierte texto que viene de Telegram en un nombre de archivo o
// carpeta seguro. Nunca devuelve algo que pueda escapar del directorio destino.
// Si no queda nada utilizable devuelve "".
func SanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f: // caracteres de control
			return -1
		case strings.ContainsRune(unsafeNameChars, r):
			return '_'
		default:
			return r
		}
	}, name)

	// Los puntos y espacios al borde generan nombres ocultos, "." o "..".
	name = strings.Trim(strings.TrimSpace(name), " .")
	if name == "" {
		return ""
	}
	return truncateName(name)
}

// truncateName recorta a MaxNameLen runas conservando la extensión.
func truncateName(name string) string {
	if utf8.RuneCountInString(name) <= MaxNameLen {
		return name
	}

	ext := filepath.Ext(name)
	if utf8.RuneCountInString(ext) > 10 { // no parece una extensión real
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

// UniquePath agrega " (1)", " (2)"... si el destino ya existe, para no pisar
// un archivo previo.
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
	return "", fmt.Errorf("demasiados archivos con el nombre %q", filepath.Base(path))
}
