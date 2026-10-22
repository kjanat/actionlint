package main

import (
	"bytes"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

func TestMetadataIsPackagedAtComponentRoot(t *testing.T) {
	component := []byte{0, 'a', 's', 'm', 13, 0, 1, 0}
	metadata := []byte(`{"metadata_version":1}`)
	packaged, err := packageMetadata(component, metadata)
	if err != nil {
		t.Fatal(err)
	}
	kind, found, err := artifact.Sniff(packaged)
	if err != nil || kind != artifact.Component || !bytes.Equal(found, metadata) {
		t.Fatalf("root metadata=%s kind=%v err=%v", found, kind, err)
	}
	if _, err := packageMetadata(packaged, metadata); err == nil {
		t.Fatal("duplicate metadata accepted")
	}
	if _, err := packageMetadata(component, []byte("{")); err == nil {
		t.Fatal("invalid metadata accepted")
	}
}
