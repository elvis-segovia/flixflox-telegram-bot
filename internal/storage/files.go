package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// MoveFile moves src to dst, first with a rename (instantaneous when they
// share a file system) and, failing that, by copying and deleting the
// original. Freeing the original matters: the local server does not clean its
// cache on its own.
func MoveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		// The file is already safe at the destination; this is only leftover
		// garbage.
		return fmt.Errorf("copied, but the original could not be deleted: %w", err)
	}
	return nil
}

// copyFile copies src to dst atomically: it writes to a .part temporary file
// and only renames it at the end, so a half-written file never shows up in
// uploads/.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	return writeAtomic(dst, in)
}

// DownloadFile fetches the file over HTTP. It is only needed against the
// official Bot API, or when the local volume is not mounted.
func DownloadFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %s", resp.Status)
	}
	return writeAtomic(dst, resp.Body)
}

func writeAtomic(dst string, r io.Reader) error {
	tmp := dst + ".part"

	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	// Close explicitly before the rename: io.Copy can leave data in the OS
	// buffer, and Close is what reports write errors (a full disk).
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
