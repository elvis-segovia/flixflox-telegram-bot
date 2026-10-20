package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveFileRemovesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")

	if err := writeAtomic(src, strings.NewReader("contenido")); err != nil {
		t.Fatal(err)
	}
	if err := MoveFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "contenido" {
		t.Errorf("destino = %q, err = %v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("el archivo original no se borró")
	}
}
