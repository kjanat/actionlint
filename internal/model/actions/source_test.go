package action

import "testing"

func TestOriginalSourceSurvivesInputAndMetadataMutation(t *testing.T) {
	content := []byte("name: original\ndescription: test\nruns: {using: node24, main: index.js}\n")
	want := string(content)
	metadata, err := Parse("action.yml", content)
	if err != nil {
		t.Fatal(err)
	}
	content[0], metadata.Source[1] = '!', '?'
	metadata.Root.Content = nil
	got, ok := metadata.OriginalSource()
	if !ok || got != want {
		t.Fatal("original parse text followed caller mutations")
	}
	metadataCopy := *metadata
	if copied, ok := metadataCopy.OriginalSource(); !ok || copied != want {
		t.Fatal("lookup descriptor lost original source provenance")
	}
	unparsed := Metadata{Source: []byte(want)}
	if _, ok := unparsed.OriginalSource(); ok {
		t.Fatal("unparsed metadata claimed source provenance")
	}
}
