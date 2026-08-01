package scan

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"sort"
	"strings"
)

type kv struct {
	k, v string
}

var pngSig = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// ExtractPrompt streams the metadata out of an image and returns the
// combined raw string (for display / fallback use). For structured parsing
// (positive/negative/params) use ExtractPromptParts instead.
func ExtractPrompt(path string) string {
	entries, err := extractEntries(path)
	if err != nil {
		return ""
	}
	return combine(entries)
}

// ExtractPromptParts extracts the raw metadata from the container and
// interprets it via ParsePrompt, returning a structured Positive/Negative/
// Params result. The second return value is false when the image has no
// usable metadata.
func ExtractPromptParts(path string) (Prompt, bool) {
	entries, err := extractEntries(path)
	if err != nil {
		return Prompt{}, false
	}
	raw := bestRaw(entries)
	if raw == "" {
		return Prompt{}, false
	}
	return ParsePrompt(raw), true
}

// ExtractPromptData reads an image's embedded generation metadata once and
// derives both the raw prompt (stored for display) and the text that should
// be embedded for similarity search (positive prompt only, when the blob is
// parseable). Calling this once and deriving both values from the same
// entries replaces what used to be two independent calls (ExtractPrompt +
// ExtractPromptParts), each of which re-opened and re-parsed the file's
// metadata chunks from disk on its own — i.e. every file paid for the same
// disk read and chunk parse twice on every scan.
func ExtractPromptData(path string) (rawPrompt, embedText string) {
	entries, err := extractEntries(path)
	if err != nil || len(entries) == 0 {
		return "", ""
	}
	rawPrompt = combine(entries)
	embedText = rawPrompt
	if raw := bestRaw(entries); raw != "" {
		if pt := ParsePrompt(raw).EmbeddingText(); pt != "" {
			embedText = pt
		}
	}
	return rawPrompt, embedText
}

// extractEntries reads the raw metadata entries from an image container
// (PNG streaming chunks or JPEG APP segments) without interpreting their
// meaning. Container parsing and prompt semantics are deliberately separate
// concerns so each can evolve independently.
func extractEntries(path string) ([]kv, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig); err != nil {
		return nil, err
	}

	switch {
	case bytes.Equal(sig, pngSig):
		return pngEntries(f)
	default:
		// JPEG metadata lives near the start of the file, so seek back and
		// read with a cap instead of loading a potentially large JPEG fully
		// into memory for every file.
		_, _ = f.Seek(0, io.SeekStart)
		data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024))
		if err != nil {
			return nil, err
		}
		if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
			return jpegEntries(data), nil
		}
	}
	return nil, nil
}

// bestRaw picks the single most useful raw metadata entry for parsing: the
// highest-priority key (parameters/prompt/...), and among ties the longest
// value. A1111/ComfyUI blobs must be parsed as one coherent string, not
// concatenated, so we deliberately take one entry instead of combine().
func bestRaw(entries []kv) string {
	if len(entries) == 0 {
		return ""
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if priority(e.k) < priority(best.k) ||
			(priority(e.k) == priority(best.k) && len(e.v) > len(best.v)) {
			best = e
		}
	}
	return best.v
}

// ---- PNG (streaming parse, skipping IDAT) --------------------------------

func pngEntries(f *os.File) ([]kv, error) {
	var entries []kv
	hdr := make([]byte, 8)

	for {
		if _, err := io.ReadFull(f, hdr); err != nil {
			break
		}
		length := binary.BigEndian.Uint32(hdr[0:4])
		typ := string(hdr[4:8])

		if typ == "IEND" {
			break
		}

		switch typ {
		case "tEXt":
			data := make([]byte, length)
			if _, err := io.ReadFull(f, data); err != nil {
				break
			}
			if idx := bytes.IndexByte(data, 0); idx >= 0 {
				entries = append(entries, kv{string(data[:idx]), string(data[idx+1:])})
			}
			f.Seek(4, io.SeekCurrent) // skip CRC
		case "iTXt":
			data := make([]byte, length)
			if _, err := io.ReadFull(f, data); err != nil {
				break
			}
			if k, v, ok := parseITXt(data); ok {
				entries = append(entries, kv{k, v})
			}
			f.Seek(4, io.SeekCurrent)
		case "zTXt":
			data := make([]byte, length)
			if _, err := io.ReadFull(f, data); err != nil {
				break
			}
			if k, v, ok := parseZTXt(data); ok {
				entries = append(entries, kv{k, v})
			}
			f.Seek(4, io.SeekCurrent)
		default:
			// Skip the chunk body and CRC in O(1) without loading it into
			// memory; we only care about the text chunks handled above.
			if _, err := f.Seek(int64(length)+4, io.SeekCurrent); err != nil {
				break
			}
		}
	}
	return entries, nil
}

func parseITXt(chunk []byte) (string, string, bool) {
	idx := bytes.IndexByte(chunk, 0)
	if idx < 0 {
		return "", "", false
	}
	k := string(chunk[:idx])
	rest := chunk[idx+1:]
	if len(rest) < 2 {
		return k, "", true
	}
	compFlag := rest[0]
	rest = rest[2:]

	li := bytes.IndexByte(rest, 0)
	if li < 0 {
		return k, "", true
	}
	rest = rest[li+1:]

	ti := bytes.IndexByte(rest, 0)
	if ti < 0 {
		return k, "", true
	}
	rest = rest[ti+1:]

	if compFlag == 1 {
		d, err := zlib.NewReader(bytes.NewReader(rest))
		if err != nil {
			return k, "", false // zlib stream failed to initialize
		}
		defer d.Close()
		dec, err := io.ReadAll(d)
		if err != nil {
			return k, "", false // decompression failed
		}
		rest = dec
	}
	return k, string(rest), true
}

func parseZTXt(chunk []byte) (string, string, bool) {
	idx := bytes.IndexByte(chunk, 0)
	if idx < 0 {
		return "", "", false
	}
	k := string(chunk[:idx])
	rest := chunk[idx+1:]
	if len(rest) < 1 {
		return k, "", true
	}

	d, err := zlib.NewReader(bytes.NewReader(rest[1:]))
	if err != nil {
		return k, "", false
	}
	defer d.Close()

	dec, err := io.ReadAll(d)
	if err != nil {
		return k, "", false
	}
	return k, string(dec), true
}

// ---- JPEG ---------------------------------------------------------------

func jpegEntries(data []byte) []kv {
	var entries []kv
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA {
			break
		}
		if marker == 0xFF || marker == 0x00 {
			i++
			continue
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if segLen < 2 {
			break
		}
		start := i + 4
		end := start + segLen - 2
		if end > len(data) {
			end = len(data)
		}
		switch marker {
		case 0xFE:
			entries = append(entries, kv{"comment", string(data[start:end])})
		case 0xE1:
			if txt := exifText(data[start:end]); txt != "" {
				entries = append(entries, kv{"parameters", txt})
			}
		}
		i = end
	}
	return entries
}

func exifText(app1 []byte) string {
	off := 0
	if len(app1) >= 6 && bytes.Equal(app1[:6], []byte("Exif\x00\x00")) {
		off = 6
	}
	tiff := app1[off:]
	if len(tiff) < 8 {
		return ""
	}
	le := tiff[0] == 'I' && tiff[1] == 'I'
	ifd0 := int(rdUint32(tiff, 4, le))
	if txt := readIFDText(tiff, ifd0, le); txt != "" {
		return txt
	}
	return ""
}

func readIFDText(tiff []byte, ifdOffset int, le bool) string {
	if ifdOffset < 0 || ifdOffset+2 > len(tiff) {
		return ""
	}
	count := int(rdUint16(tiff, ifdOffset, le))
	pos := ifdOffset + 2
	var exifSub int = -1
	var desc string
	for i := 0; i < count; i++ {
		if pos+12 > len(tiff) {
			break
		}
		tag := rdUint16(tiff, pos, le)
		switch tag {
		case 0x010E:
			if v := readASCII(tiff, pos, le); v != "" {
				desc = v
			}
		case 0x8769:
			exifSub = int(rdUint32(tiff, pos+8, le))
		case 0x9286:
			if v := readUserComment(tiff, pos, le); v != "" {
				return v
			}
		}
		pos += 12
	}
	if exifSub >= 0 {
		if v := readIFDText(tiff, exifSub, le); v != "" {
			return v
		}
	}
	return desc
}

func readASCII(tiff []byte, entry int, le bool) string {
	count := int(rdUint32(tiff, entry+4, le))
	var data []byte
	if count <= 4 {
		if entry+8+count > len(tiff) {
			return ""
		}
		data = tiff[entry+8 : entry+8+count]
	} else {
		off := int(rdUint32(tiff, entry+8, le))
		if off+count > len(tiff) {
			return ""
		}
		data = tiff[off : off+count]
	}
	return strings.TrimRight(string(data), "\x00")
}

func readUserComment(tiff []byte, entry int, le bool) string {
	count := int(rdUint32(tiff, entry+4, le))
	if count <= 8 {
		return ""
	}
	off := int(rdUint32(tiff, entry+8, le))
	if off+count > len(tiff) {
		return ""
	}
	body := tiff[off : off+count]
	cs := string(body[:8])
	text := body[8:]
	switch {
	case strings.HasPrefix(cs, "UNICODE"):
		return strings.TrimRight(decodeUTF16(text, le), "\x00")
	case strings.HasPrefix(cs, "JIS"):
		return ""
	default:
		return strings.TrimRight(string(text), "\x00")
	}
}

func decodeUTF16(b []byte, le bool) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	var out []rune
	for i := 0; i+1 < len(b); i += 2 {
		var u uint16
		if le {
			u = uint16(b[i]) | uint16(b[i+1])<<8
		} else {
			u = uint16(b[i+1]) | uint16(b[i])<<8
		}
		out = append(out, rune(u))
	}
	return string(out)
}

func rdUint16(b []byte, off int, le bool) uint16 {
	if off+2 > len(b) {
		return 0
	}
	if le {
		return binary.LittleEndian.Uint16(b[off:])
	}
	return binary.BigEndian.Uint16(b[off:])
}

func rdUint32(b []byte, off int, le bool) uint32 {
	if off+4 > len(b) {
		return 0
	}
	if le {
		return binary.LittleEndian.Uint32(b[off:])
	}
	return binary.BigEndian.Uint32(b[off:])
}

// priority ranks a metadata key so the most useful prompt source wins.
func priority(k string) int {
	switch strings.ToLower(k) {
	case "parameters":
		return 0
	case "positive prompt", "positive":
		return 1
	case "prompt":
		return 2
	case "description", "image_description":
		return 3
	case "comment":
		return 4
	}
	if strings.Contains(strings.ToLower(k), "prompt") {
		return 2
	}
	return 5
}

func combine(entries []kv) string {
	if len(entries) == 0 {
		return ""
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return priority(entries[i].k) < priority(entries[j].k)
	})
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.v)
		b.WriteByte('\n')
	}
	s := b.String()

	// 1 MiB cap: large enough for verbose ComfyUI JSON workflow graphs.
	const maxLen = 1024 * 1024
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return strings.TrimSpace(s)
}
