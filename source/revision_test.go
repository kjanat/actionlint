package source

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRevision(t *testing.T) {
	if (Revision{}).Valid() {
		t.Fatal("zero revision is valid")
	}
	digest := strings.Repeat("a1", 32)
	revision, err := NewRevision("<stdin>", digest)
	if err != nil || !revision.Valid() || revision.Document() != "<stdin>" || revision.Digest() != digest {
		t.Fatalf("revision = %#v, %v", revision, err)
	}
	for _, input := range [][2]string{
		{"", digest}, {"a\x00b", digest}, {"document", ""},
		{"document", strings.ToUpper(digest)}, {"document", strings.Repeat("z", 64)},
	} {
		if value, err := NewRevision(input[0], input[1]); err == nil || value.Valid() {
			t.Errorf("accepted invalid revision %q", input)
		}
	}
}

func TestRevisionJSON(t *testing.T) {
	revision, err := NewRevision("<stdin>", strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"document":"<stdin>","digest":"` + revision.Digest() + `"}`
	// encoding/json escapes angle brackets by default.
	want = strings.ReplaceAll(strings.ReplaceAll(want, "<", `\u003c`), ">", `\u003e`)
	if string(encoded) != want {
		t.Fatalf("encoded = %s", encoded)
	}
	var decoded Revision
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != revision {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
	for _, data := range []string{`{}`, `null`, `{"document":"file","digest":"bad"}`, `{"document":"","digest":"` + revision.Digest() + `"}`, `[]`} {
		decoded = revision
		if err := json.Unmarshal([]byte(data), &decoded); err == nil || decoded != revision {
			t.Errorf("invalid JSON changed receiver: %s, %v", data, err)
		}
	}
	if _, err := json.Marshal(Revision{}); err == nil {
		t.Fatal("zero revision encoded successfully")
	}
}
