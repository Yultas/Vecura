package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type manifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
	Notes     string `json:"notes,omitempty"`
}

func main() {
	file := flag.String("file", "", "executable to hash")
	version := flag.String("version", "", "release version")
	url := flag.String("url", "", "release executable URL")
	notes := flag.String("notes", "", "release notes")
	out := flag.String("out", "manifest.json", "manifest output path")
	flag.Parse()
	if *file == "" || *version == "" || *url == "" {
		fatal("file, version and url are required")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		fatal(err.Error())
	}
	sum := sha256.Sum256(data)
	m := manifest{Version: strings.TrimPrefix(*version, "v"), URL: *url, SHA256: hex.EncodeToString(sum[:]), Notes: *notes}
	privateB64 := os.Getenv("VECURA_UPDATE_PRIVATE_KEY")
	if privateB64 == "" {
		fatal("VECURA_UPDATE_PRIVATE_KEY is not set")
	}
	privateBytes, err := base64.StdEncoding.DecodeString(privateB64)
	if err != nil || len(privateBytes) != ed25519.PrivateKeySize {
		fatal("VECURA_UPDATE_PRIVATE_KEY must be base64-encoded Ed25519 private key")
	}
	private := ed25519.PrivateKey(privateBytes)
	payload := []byte(m.Version + "\n" + m.URL + "\n" + strings.ToLower(m.SHA256))
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		fatal(err.Error())
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
