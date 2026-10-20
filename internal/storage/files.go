package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// MoveFile mueve src a dst, primero con rename (instantáneo si comparten
// sistema de archivos) y si no copiando y borrando el original. Liberar el
// original importa: el servidor local no limpia su cache por sí solo.
func MoveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		// El archivo ya está a salvo en destino; esto es solo basura que queda.
		return fmt.Errorf("copiado, pero no se pudo borrar el original: %w", err)
	}
	return nil
}

// copyFile copia src a dst de forma atómica: escribe a un temporal .part y
// recién al final lo renombra, así nunca queda un archivo a medias en uploads/.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	return writeAtomic(dst, in)
}

// DownloadFile baja el archivo por HTTP. Solo hace falta contra el Bot API
// oficial o si el volumen local no está montado.
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
		return fmt.Errorf("descarga falló con estado %s", resp.Status)
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
	// Cerrar explícitamente antes del rename: io.Copy puede dejar datos en el
	// buffer del SO y Close es el que reporta errores de escritura (disco lleno).
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
