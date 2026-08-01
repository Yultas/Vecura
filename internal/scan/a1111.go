package scan

import (
	"encoding/json"
	"strings"
)

// Prompt is the structured result of interpreting an image's generation
// metadata. EmbeddingText returns the slice of text that should be fed to the
// embedding model: for A1111 that is the positive prompt only — the negative
// prompt and the machine settings (Steps/Sampler/Seed/...) are noise for
// semantic search and would degrade similarity results.
type Prompt struct {
	Positive string
	Negative string
	Params   map[string]string
	Raw      string
}

// EmbeddingText returns the text that should be embedded for similarity search.
func (p Prompt) EmbeddingText() string {
	if s := strings.TrimSpace(p.Positive); s != "" {
		return s
	}
	return strings.TrimSpace(p.Raw)
}

// ParsePrompt flexibly interprets a raw metadata blob. It detects the major
// authoring formats (Automatic1111, ComfyUI JSON, NovelAI) and otherwise
// falls back to treating the whole blob as a positive prompt.
func ParsePrompt(raw string) Prompt {
	raw = strings.Trim(raw, "\r\n\t ")
	if raw == "" {
		return Prompt{}
	}
	// ComfyUI / AnythingV2 export a JSON workflow.
	if isJSONPrefix(raw) {
		if p, ok := parseComfyUI(raw); ok {
			p.Raw = raw
			return p
		}
	}
	// Automatic1111 / Forge / SD.Next use "Negative prompt:" as a marker.
	if p, ok := parseA1111(raw); ok {
		p.Raw = raw
		return p
	}
	// NovelAI and everything else: a single positive prompt.
	return Prompt{Positive: raw, Raw: raw}
}

// isJSONPrefix reports whether a trimmed blob looks like JSON.
func isJSONPrefix(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// parseA1111 splits an Automatic1111 parameters blob into positive prompt,
// negative prompt and a key/value map of generation parameters.
//
// Layout:
//
//	<positive, possibly multiline>
//	Negative prompt: <negative>
//	<Key>: <value>, <Key2>: <value2>, ...
func parseA1111(raw string) (Prompt, bool) {
	lines := strings.Split(raw, "\n")
	negIdx := -1
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "Negative prompt:") {
			negIdx = i
			break
		}
	}
	if negIdx < 0 {
		return Prompt{}, false
	}

	positive := strings.TrimSpace(strings.Join(lines[:negIdx], "\n"))

	negLine := strings.TrimSpace(lines[negIdx])
	negBody := strings.TrimSpace(strings.TrimPrefix(negLine, "Negative prompt:"))
	negBody = strings.TrimPrefix(negBody, ":")
	negBody = strings.TrimSpace(negBody)

	// Remaining lines hold the generation parameters (usually a single
	// comma-separated line).
	rest := strings.TrimSpace(strings.Join(lines[negIdx+1:], "\n"))
	// The negative prompt may run into the params on the same line when the
	// metadata was flattened. In that case the params start at the first
	// "Key: Value" style token.
	if negBody == "" && rest != "" {
		negBody, rest = splitNegativeFromParams(rest)
	}

	params := parseParams(rest)
	return Prompt{
		Positive: positive,
		Negative: negBody,
		Params:   params,
	}, true
}

// splitNegativeFromParams handles a flattened "Negative prompt: <neg> <K>: <V>"
// line by detecting where the structured params begin.
func splitNegativeFromParams(s string) (string, string) {
	idx := strings.Index(s, ", ")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// parseParams decodes a "Key: Value, Key2: Value2" string into a map. It is
// quote-aware so values containing commas (e.g. Lora hashes) are kept intact.
func parseParams(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" {
		return out
	}
	for _, tok := range splitTopLevel(s, ',') {
		kv := strings.SplitN(tok, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, `"`)
		if key != "" {
			out[key] = val
		}
	}
	return out
}

// splitTopLevel splits s on sep, but ignores separators that occur inside a
// double-quoted span.
func splitTopLevel(s string, sep rune) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == sep && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// parseComfyUI extracts a positive (and optionally negative) prompt from a
// ComfyUI workflow/API JSON. It is intentionally heuristic: it collects every
// textual node input, treats the longest as the positive prompt and, when a
// second substantial text input exists, treats it as the negative prompt.
func parseComfyUI(raw string) (Prompt, bool) {
	var doc struct {
		Nodes []struct {
			Inputs struct {
				Text string `json:"text"`
			} `json:"inputs"`
			ClassType string `json:"class_type"`
		} `json:"nodes"`
	}
	// API format uses node-id keys rather than a "nodes" array.
	var apiFormat map[string]struct {
		Inputs struct {
			Text string `json:"text"`
		} `json:"inputs"`
		ClassType string `json:"class_type"`
	}

	texts := []string{}
	ingest := func(text, class string) {
		t := strings.TrimSpace(text)
		if t == "" {
			return
		}
		// Skip pure node identifiers / tiny tokens.
		if len(t) < 3 {
			return
		}
		texts = append(texts, t)
	}

	if err := json.Unmarshal([]byte(raw), &doc); err == nil && len(doc.Nodes) > 0 {
		for _, n := range doc.Nodes {
			ingest(n.Inputs.Text, n.ClassType)
		}
	} else if err := json.Unmarshal([]byte(raw), &apiFormat); err == nil && len(apiFormat) > 0 {
		for _, n := range apiFormat {
			ingest(n.Inputs.Text, n.ClassType)
		}
	} else {
		return Prompt{}, false
	}

	if len(texts) == 0 {
		return Prompt{}, false
	}

	// Longest text is the positive prompt; a second distinct long text is the
	// negative prompt.
	longest, second := pickLongestTwo(texts)
	p := Prompt{Positive: longest}
	if second != "" {
		p.Negative = second
	}
	return p, true
}

// pickLongestTwo returns the two longest, distinct strings from the slice.
func pickLongestTwo(in []string) (string, string) {
	a, b := "", ""
	for _, s := range in {
		if len(s) > len(a) {
			b, a = a, s
		} else if len(s) > len(b) && s != a {
			b = s
		}
	}
	return a, b
}
