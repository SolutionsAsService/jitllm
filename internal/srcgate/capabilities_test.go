package srcgate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jitllm/jitllm/internal/capabilities"
	"github.com/jitllm/jitllm/internal/testmodels"
)

// TestCapabilitiesDocIsGenerated holds docs/capabilities.json to the code: the
// converter's lists, quant's types, the container version, maintained.json
// and the server's api.json (itself held to the route table by
// server.TestAPIManifestIsGenerated). An architecture added, a quant type
// admitted or a field edited by hand fails it until the generator runs.
func TestCapabilitiesDocIsGenerated(t *testing.T) {
	testmodels.SourceTree(t)
	m, err := capabilities.Doc()
	if err != nil {
		t.Fatal(err)
	}
	want, err := capabilities.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(capabilities.Path)))
	if err != nil {
		t.Fatalf("%v; run: %s", err, capabilities.Command)
	}
	if !bytes.Equal(have, want) {
		t.Fatalf("%s is not what the code generates; run: %s", capabilities.Path, capabilities.Command)
	}
}
