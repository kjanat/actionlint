package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/render"

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
	version, err := exec.CommandContext(t.Context(), jq, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("jq version: %s: %v", version, err)
	}
	t.Logf("jq: %s (%s)", jq, bytes.TrimSpace(version))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	data := []byte(`{"name":"actionlint","list":[true,"résumé"]}`)
	plain, err := formatJSONWithJQ(ctx, jq, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(plain) || bytes.Count(plain, []byte("\n")) < 4 || bytes.ContainsRune(plain, '\x1b') {
		t.Fatalf("jq did not pretty-print plain JSON: %q", plain)
	}
	t.Setenv("NO_COLOR", "1")
	colored, err := formatJSONWithJQ(ctx, jq, data, true)
	if err != nil || !bytes.Contains(colored, []byte("\x1b[")) {
		t.Fatalf("explicit color not honored: %q, %v", colored, err)
	}
	canceled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := formatJSONWithJQ(canceled, jq, data, false); !errors.Is(err, context.Canceled) {
		t.Fatal("jq ignored cancellation")
	}
}

func TestJSONFormatterCallerDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_JSON_FORMATTER", "slow")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := formatJSONWithJQ(ctx, executable, []byte("{}"), true)
	if err != nil || !bytes.Contains(output, []byte("\x1b[")) {
		t.Fatalf("caller deadline was replaced: %q, %v", output, err)
	}

	expired, stop := context.WithTimeout(t.Context(), time.Millisecond)
	defer stop()
	if _, err := formatJSONWithJQ(expired, executable, []byte("{}"), true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("formatter deadline cause lost: %v", err)
	}
}

func TestJSONFormatterFailureDetails(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_JSON_FORMATTER", "error")
	_, err = formatJSONWithJQ(t.Context(), executable, []byte("{}"), true)
	if err == nil || !strings.Contains(err.Error(), "formatter fixture failed") {
		t.Fatalf("formatter stderr lost: %v", err)
	}
}

func jsonFormatterHelper(mode string) int {
	if mode == "error" {
		fmt.Fprintln(os.Stderr, "formatter fixture failed")
		return 1
	}
	if len(os.Args) != 3 || os.Args[1] != "--color-output" || os.Args[2] != "." {
		return 2
	}
	time.Sleep(2100 * time.Millisecond)
	fmt.Print("\x1b[32m{}\x1b[0m\n")
	return 0
}
