package capabilities

import (
	"bytes"
	"encoding/json"
)

// Render is a manifest as the file holds it: indented, HTML characters left
// as they are, one trailing newline.
func Render(m Manifest) ([]byte, error) { return indent(m) }

// RenderAPI is api.json's bytes for a.
func RenderAPI(a API) ([]byte, error) { return indent(a) }

func indent(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
