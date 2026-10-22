package shellcheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

func TestShellcheckConfigDirectorySelection(t *testing.T) {
	root := t.TempDir()
	if _, err := shellcheckRCFile(fsread.New(t.Context(), nil, nil), root); err == nil {
		t.Fatal("accepted empty config directory")
	}
	writeShellcheckFixture(t, root, "shellcheckrc", "enable=all\n")
	dotfile := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	got, err := shellcheckRCFile(fsread.New(t.Context(), nil, nil), root)
	if err != nil || got != dotfile {
		t.Fatalf("want dotfile first, got %q: %v", got, err)
	}
}
