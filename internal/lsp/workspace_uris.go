package lsp

import (
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// WorkspaceURIs keeps the URI scheme, authority and revision query sent back to the client.
type WorkspaceURIs struct {
	root      url.URL
	directory string
}

// NewWorkspaceURIs accepts hierarchical URIs such as vscode-vfs, and directory need not exist.
func NewWorkspaceURIs(rootURI, directory string) (WorkspaceURIs, error) {
	root, err := url.Parse(rootURI)
	if err != nil {
		return WorkspaceURIs{}, err
	}
	if root.Scheme == "" || root.Opaque != "" || root.User != nil || root.Fragment != "" || !strings.HasPrefix(root.Path, "/") {
		return WorkspaceURIs{}, errors.New("workspace requires an absolute hierarchical URI")
	}
	if !filepath.IsAbs(directory) {
		return WorkspaceURIs{}, errors.New("workspace directory must be absolute")
	}
	root.Path, root.RawPath = path.Clean(root.Path), ""
	return WorkspaceURIs{root: *root, directory: filepath.Clean(directory)}, nil
}

func (w *WorkspaceURIs) Directory() string { return w.directory }

func (w *WorkspaceURIs) Path(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != w.root.Scheme || u.Host != w.root.Host || u.User != nil || u.RawQuery != w.root.RawQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("document URI belongs to another workspace")
	}
	prefix := strings.TrimSuffix(w.root.Path, "/") + "/"
	clean := path.Clean(u.Path)
	if clean != w.root.Path && !strings.HasPrefix(clean, prefix) {
		return "", errors.New("document URI is outside the workspace")
	}
	relative := strings.TrimPrefix(clean, prefix)
	if clean == w.root.Path {
		relative = "."
	}
	if strings.ContainsAny(relative, "\\\x00") {
		return "", errors.New("document URI contains an invalid path character")
	}
	return filepath.Join(w.directory, filepath.FromSlash(relative)), nil
}

// URI returns an empty string for a path outside the workspace.
func (w *WorkspaceURIs) URI(file string) string {
	relative, err := filepath.Rel(w.directory, file)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	u := w.root
	u.Path = path.Join(w.root.Path, filepath.ToSlash(relative))
	return u.String()
}
