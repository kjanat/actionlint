package langserver

import (
	"errors"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// WorkspaceURIs maps one client workspace to host paths without changing the
// URI scheme, authority or revision query sent back to the client.
type WorkspaceURIs struct {
	root      url.URL
	directory string
}

// NewWorkspaceURIs accepts hierarchical workspace URIs, including vscode-vfs.
// Directory is an absolute host path; it need not exist on a filesystem.
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

// Path resolves a document URI within this workspace to its virtual host path.
func (w WorkspaceURIs) Path(uri string) (string, error) {
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

// URI returns the client's URI for a virtual host path, or an empty string for
// a path outside the workspace. Hosts must not request files outside that root.
func (w WorkspaceURIs) URI(file string) string {
	relative, err := filepath.Rel(w.directory, file)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	u := w.root
	u.Path = path.Join(w.root.Path, filepath.ToSlash(relative))
	return u.String()
}
