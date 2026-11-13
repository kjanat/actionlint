package schema

import (
	"bytes"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestEmbeddedSchemasMatchPublishedFiles(t *testing.T) {
	t.Parallel()
	root := os.DirFS("../../../..")
	names, err := fs.Glob(root, "schemas/*/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no published tool or result schemas found")
	}
	names = append(names, "actionlint.schema.json")
	slices.Sort(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			published, err := fs.ReadFile(root, name)
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := Schema(name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(published, embedded) {
				t.Fatalf("embedded %s differs from its published file; run mise run generate:config-schema", name)
			}
		})
	}
	embedded, err := fs.Glob(schemaFiles, "assets/schemas/*/*.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	embedded = append(embedded, "assets/actionlint.schema.json")
	for i, name := range embedded {
		embedded[i] = strings.TrimPrefix(name, "assets/")
	}
	slices.Sort(embedded)
	if !slices.Equal(names, embedded) {
		t.Fatalf("embedded schema paths differ from published paths: published=%v embedded=%v", names, embedded)
	}
}
