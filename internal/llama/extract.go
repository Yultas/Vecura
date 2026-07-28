package llama

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractZip unpacks src into destDir. Existing files are overwritten.
func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	for _, f := range r.File {
		// Normalize: zip may contain a top-level directory — strip it.
		name := f.Name
		parts := strings.SplitN(name, "/", 2)
		if len(parts) > 1 {
			name = parts[1]
		}
		if name == "" {
			continue
		}

		target := filepath.Join(destDir, filepath.Clean(name))
		// Prevent zip-slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		outFile, err := os.Create(target)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
		// Make executables actually executable on Unix (no-op on Windows).
		_ = os.Chmod(target, 0o755)
	}
	return nil
}
