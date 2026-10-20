package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/render"

	"golang.org/x/sys/execabs"
)

func TestTerminalJSONKeepsRedirectedBytes(t *testing.T) {
	data := []byte("{\"unicode\":\"résumé\",\"count\":2}\n")
	var out bytes.Buffer
	if err := writePrettyJSON(t.Context(), &out, render.ColorAlways, data, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("buffer output changed: %q", &out)
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := writePrettyJSON(t.Context(), file, render.ColorAlways, data, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file output changed: %q, %v", got, err)
	}
	if err := writePrettyJSON(t.Context(), commandFailingIO{}, render.ColorAuto, data, false); err == nil {
		t.Fatal("lost output error")
	}
}

func TestTerminalJSONJQ(t *testing.T) {
	jq, err := execabs.LookPath("jq")
	if err != nil {
		t.Skip("jq not installed")
	}
	data := []byte(`{"name":"actionlint","list":[true,"résumé"]}`)
	plain, err := formatJSONWithJQ(t.Context(), jq, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(plain) || bytes.Count(plain, []byte("\n")) < 4 || bytes.ContainsRune(plain, '\x1b') {
		t.Fatalf("jq did not pretty-print plain JSON: %q", plain)
	}
	t.Setenv("NO_COLOR", "1")
	colored, err := formatJSONWithJQ(t.Context(), jq, data, true)
	if err != nil || !bytes.Contains(colored, []byte("\x1b[")) {
		t.Fatalf("explicit color not honored: %q, %v", colored, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := formatJSONWithJQ(ctx, jq, data, false); err == nil {
		t.Fatal("jq ignored cancellation")
	}
}
