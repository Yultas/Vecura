package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestURL is the stable URL of the latest GitHub Release manifest.
const ManifestURL = "https://github.com/Yultas/Vecura/releases/latest/download/manifest.json"

// PublicKeyBase64 is injected at release build time. It is intentionally empty
// in development builds so local builds can still run without signing keys.
var PublicKeyBase64 = ""

type Manifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
	Notes     string `json:"notes,omitempty"`
}

func (m Manifest) Payload() []byte {
	return []byte(m.Version + "\n" + m.URL + "\n" + strings.ToLower(m.SHA256))
}

func FetchManifest() (Manifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(ManifestURL)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update manifest returned HTTP %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	if m.Version == "" || m.URL == "" || m.SHA256 == "" {
		return Manifest{}, fmt.Errorf("update manifest is incomplete")
	}
	return m, nil
}

func VerifyManifest(m Manifest) error {
	if !strings.HasPrefix(m.URL, "https://github.com/Yultas/Vecura/releases/download/") {
		return fmt.Errorf("update URL is not trusted")
	}
	want, err := hex.DecodeString(strings.TrimSpace(m.SHA256))
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("invalid update SHA-256")
	}
	if m.Signature == "" {
		if PublicKeyBase64 != "" {
			return fmt.Errorf("update manifest has no signature")
		}
		return nil
	}
	if PublicKeyBase64 == "" {
		return fmt.Errorf("update signing key is not configured")
	}
	pubBytes, err := base64.StdEncoding.DecodeString(PublicKeyBase64)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid update public key")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid update signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), m.Payload(), sig) {
		return fmt.Errorf("update signature verification failed")
	}
	return nil
}

func DownloadAndVerify(m Manifest, dir string) (path string, err error) {
	if err := VerifyManifest(m); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create update directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "Vecura-update-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create update file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(m.URL)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update download returned HTTP %d", resp.StatusCode)
	}
	if _, err = io.Copy(tmp, resp.Body); err != nil {
		return "", fmt.Errorf("write update: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("close update: %w", err)
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), m.SHA256) {
		return "", fmt.Errorf("downloaded update SHA-256 does not match manifest")
	}
	finalPath := filepath.Join(dir, "Vecura-update.exe")
	if err = os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err = os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("prepare update: %w", err)
	}
	return finalPath, nil
}
