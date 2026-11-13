package lsp

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceURIsPreserveVirtualIdentity(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "virtual-workspace")
	for _, root := range []string{"file:///workspace", "vscode-vfs://github/owner/repo?ref=master"} {
		workspace, err := NewWorkspaceURIs(root, directory)
		if err != nil {
			t.Fatal(err)
		}
		var document string
		if root == "file:///workspace" {
			document = root + "/.github/workflows/a%20b-%F0%9F%9A%80.yml"
		} else {
			document = "vscode-vfs://github/owner/repo/.github/workflows/a%20b-%F0%9F%9A%80.yml?ref=master"
		}
		path, err := workspace.Path(document)
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(directory, ".github", "workflows", "a b-🚀.yml") || workspace.URI(path) != document {
			t.Fatalf("URI identity changed: path=%s uri=%s", path, workspace.URI(path))
		}
		if workspace.URI(filepath.Join(directory, "..", "outside.yml")) != "" {
			t.Fatal("outside host path accepted")
		}
	}
}

func TestWorkspaceURIsRejectDifferentRootsAndRevisions(t *testing.T) {
	t.Parallel()
	workspace, err := NewWorkspaceURIs("vscode-vfs://github/owner/repo?ref=master", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{
		"file:///owner/repo/a.yml?ref=master",
		"vscode-vfs://another/owner/repo/a.yml?ref=master",
		"vscode-vfs://github/owner/repository/a.yml?ref=master",
		"vscode-vfs://github/owner/repo/../other/a.yml?ref=master",
		"vscode-vfs://github/owner/repo/a.yml?ref=changed",
		"vscode-vfs://github/owner/repo/a.yml?ref=master#fragment",
		"vscode-vfs://credentials@github/owner/repo/a.yml?ref=master",
		"vscode-vfs://github/owner/repo/a%5Cb.yml?ref=master",
	} {
		if _, err := workspace.Path(uri); err == nil {
			t.Errorf("foreign or invalid document accepted: %s", uri)
		}
	}
}
