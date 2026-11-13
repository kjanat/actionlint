package component

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalContractIsCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../plugins/protocol/wit/analyzer.wit")
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
	if digest != analyzerWITSHA256 {
		t.Fatal("canonical WIT changed: run go generate ./internal/plugins/execution/component")
	}
}

func TestCanonicalContractChecksCompleteShape(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"compatible-contract", "wrong-signature", "wrong-record", "wrong-enum"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", "contracts", name+".wasm"))
			if err != nil {
				t.Fatal(err)
			}
			err = validateAnalyzerInterface(data)
			if name == "compatible-contract" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "incompatible analyzer contract") {
				t.Fatalf("shape mismatch not rejected: %v", err)
			}
		})
	}
}
