package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const staleDoc = "```mermaid\n" +
	"    %% renovate: depName=example.com/fork\n" +
	"    A --> F[\"fork<br/>v1.1.0\"]\n" +
	"    %% renovate: depName=example.com/shared\n" +
	"    E --> S[\"shared<br/>v0.3.0 · upstream\"]\n" +
	"```\n"

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func goMod(module, deps string, requires ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo 1.22\n", module)
	for _, require := range requires {
		path, _, _ := strings.Cut(require, " ")
		fmt.Fprintf(&b, "\nrequire %s\n", require)
		fmt.Fprintf(&b, "replace %s => %s/%s\n", path, deps, strings.TrimPrefix(path, "example.com/"))
	}
	return b.String()
}

func moduleTree(t *testing.T, doc, sdkShared string) string {
	t.Helper()
	return writeTree(t, map[string]string{
		document:                doc,
		"deps/fork/go.mod":      "module example.com/fork\n\ngo 1.22\n",
		"deps/shared/go.mod":    "module example.com/shared\n\ngo 1.22\n",
		"go.mod":                goMod("example.com/root", "./deps", "example.com/fork v1.2.0", "example.com/shared v0.3.0"),
		"plugins/sdk/go/go.mod": goMod("example.com/sdk", "../../../deps", "example.com/shared "+sdkShared),
		"plugins/examples/insecure-download/go.mod": goMod("example.com/example", "../../../deps", "example.com/shared v0.3.0"),
	})
}

func TestRunUpdatesStaleLabels(t *testing.T) {
	t.Parallel()
	root := moduleTree(t, staleDoc, "v0.3.0")
	want := strings.Replace(staleDoc, "fork<br/>v1.1.0", "fork<br/>v1.2.0", 1)
	report := "docs/go-forks.md: example.com/fork v1.1.0 -> v1.2.0\n"

	var stdout bytes.Buffer
	err := run(context.Background(), root, true, &stdout)
	if err == nil || !strings.Contains(err.Error(), "stale version labels") {
		t.Fatalf("check reported %v", err)
	}
	if diff := cmp.Diff(report, stdout.String()); diff != "" {
		t.Fatal(diff)
	}
	if have := readDoc(t, root); have != staleDoc {
		t.Fatalf("check rewrote the document:\n%s", have)
	}

	stdout.Reset()
	if err := run(context.Background(), root, false, &stdout); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(report, stdout.String()); diff != "" {
		t.Fatal(diff)
	}
	if diff := cmp.Diff(want, readDoc(t, root)); diff != "" {
		t.Fatal(diff)
	}

	stdout.Reset()
	if err := run(context.Background(), root, true, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("current labels reported changes: %q", stdout.String())
	}
}

func TestRunIgnoresVendorModeFromGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=vendor")
	root := moduleTree(t, staleDoc, "v0.3.0")
	var stdout bytes.Buffer
	if err := run(context.Background(), root, false, &stdout); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff("docs/go-forks.md: example.com/fork v1.1.0 -> v1.2.0\n", stdout.String()); diff != "" {
		t.Fatal(diff)
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what      string
		doc       string
		sdkShared string
		want      string
	}{
		{"modules disagree", staleDoc, "v0.4.0", "example.com/shared is v0.3.0 in . and v0.4.0 in plugins/sdk/go"},
		{"module absent", "%% renovate: depName=example.com/absent\nX[\"absent<br/>v1.0.0\"]\n", "v0.3.0", "example.com/absent is in no module graph"},
		{"main module", "%% renovate: depName=example.com/root\nX[\"root<br/>v1.0.0\"]\n", "v0.3.0", "example.com/root has no version"},
		{"no markers", "# Forks\n", "v0.3.0", "no renovate depName markers"},
		{"marker on last line", "%% renovate: depName=example.com/fork", "v0.3.0", "line 1: example.com/fork has no label line"},
		{"label without version", "%% renovate: depName=example.com/fork\nF[\"fork\"]\n", "v0.3.0", "line 2: example.com/fork has no <br/>v... version label"},
	}
	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			root := moduleTree(t, tc.doc, tc.sdkShared)
			var stdout bytes.Buffer
			err := run(context.Background(), root, false, &stdout)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q but got %v", tc.want, err)
			}
			if have := readDoc(t, root); have != tc.doc {
				t.Fatalf("document changed:\n%s", have)
			}
		})
	}
}

func TestRunGoListError(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{document: staleDoc, "go.mod": "not a go.mod\n"})
	err := run(context.Background(), root, false, &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), ".: go list -m: ") {
		t.Fatalf("wanted a go list error but got %v", err)
	}
}

func TestRunMissingDocument(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), t.TempDir(), false, &bytes.Buffer{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("wanted a missing document error but got %v", err)
	}
}

func readDoc(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, document))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
