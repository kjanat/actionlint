package component

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBundledComponentMatchesSourceRecord(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("builtin_commitish.source.json")
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Repository string `json:"repository"`
		Revision   string `json:"revision"`
		SHA256     string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	artifact := BuiltinCommitishArtifact()
	if record.SHA256 != artifact.Info.SHA256 {
		t.Fatalf("component digest %s differs from recorded source build %s", artifact.Info.SHA256, record.SHA256)
	}
	if record.Repository != "https://github.com/kjanat/commitish" || len(record.Revision) != 40 {
		t.Fatalf("missing source revision: %+v", record)
	}
	artifact.Bytes[0] = 0xff
	if BuiltinCommitishArtifact().Bytes[0] != 0 {
		t.Fatal("artifact caller modified the bundled component")
	}
}
