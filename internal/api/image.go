package api

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ImageDataURI returns an indexed image as a data URL that the WebView can
// render without trying to access the user's filesystem directly.
func (a *App) ImageDataURI(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("image path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("image is empty")
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if contentType == "application/octet-stream" {
		return "", fmt.Errorf("unsupported image type: %s", filepath.Ext(path))
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
