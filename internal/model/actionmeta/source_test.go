package actionmeta

import "testing"

func TestTextSurvivesInputAndMetadataMutation(t *testing.T) {
	t.Parallel()
	content := []byte("name: original\ndescription: test\nruns: {using: node24, main: index.js}\n")
	want := string(content)
	metadata, err := ParseActionMetadata("action.yml", content)
	if err != nil {
		t.Fatal(err)
	}
	content[0] = '!'
	metadata.Root.Content = nil
	got, ok := metadata.Text()
	if !ok || got != want {
		t.Fatal("original parse text followed caller mutations")
	}
	metadataCopy := *metadata
	if copied, ok := metadataCopy.Text(); !ok || copied != want {
		t.Fatal("copied metadata lost its manifest text")
	}
	var unparsed Metadata
	if _, ok := unparsed.Text(); ok {
		t.Fatal("unparsed metadata claimed manifest text")
	}
	var absent *Metadata
	if _, ok := absent.Text(); ok {
		t.Fatal("nil metadata claimed manifest text")
	}
}
