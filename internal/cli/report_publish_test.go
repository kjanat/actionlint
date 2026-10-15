package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishNewReport(t *testing.T) {
	for _, name := range []string{"new", "existing"} {
		t.Run(name, func(t *testing.T) {
			existing := name == "existing"
			root := filepath.Join(t.TempDir(), "reports with spaces é")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			source, destination := filepath.Join(root, "temporary"), filepath.Join(root, "report.json")
			if err := os.WriteFile(source, []byte("completed report"), 0600); err != nil {
				t.Fatal(err)
			}
			want := "completed report"
			if existing {
				want = "existing report"
				if err := os.WriteFile(destination, []byte(want), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := publishNewReport(source, destination)
			if existing && !errors.Is(err, os.ErrExist) || !existing && err != nil {
				t.Fatalf("publish: %v", err)
			}
			data, err := os.ReadFile(destination)
			if err != nil || string(data) != want {
				t.Fatalf("destination = %q, %v; want %q", data, err, want)
			}
			if existing {
				data, err := os.ReadFile(source)
				if err != nil || string(data) != "completed report" {
					t.Fatalf("source changed after failed publication: %q, %v", data, err)
				}
			}
		})
	}
}
