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

	if err := writeAtomic(src, strings.NewReader("contents")); err != nil {
		t.Fatal(err)
	}
	if err := MoveFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "contents" {
		t.Errorf("destination = %q, err = %v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("the original file was not deleted")
	}
}
