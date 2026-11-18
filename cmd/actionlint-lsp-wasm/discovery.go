//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"syscall/js"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
	"actionlint.kjanat.dev/v2/internal/lsp"
)

type hostFilesystem struct {
	ctx       context.Context
	workspace lsp.WorkspaceURIs
	platform  string
}
type hostFileInfo struct {
	Filename string `json:"name"`
	Kind     string `json:"kind"`
	Bytes    int64  `json:"size"`
	Modified int64  `json:"modified"`
}

func (i hostFileInfo) Name() string { return i.Filename }
func (i hostFileInfo) Size() int64  { return i.Bytes }
func (i hostFileInfo) Mode() fs.FileMode {
	switch i.Kind {
	case "directory":
		return fs.ModeDir | 0o755
	case "symlink":
		return fs.ModeSymlink | 0o777
	case "file":
		return 0o644
	default:
		return fs.ModeIrregular
	}
}
func (i hostFileInfo) ModTime() time.Time { return time.UnixMilli(i.Modified) }
func (i hostFileInfo) IsDir() bool        { return i.Kind == "directory" }
func (i hostFileInfo) Sys() any           { return nil }

type filesystemResponse struct {
	Info    *hostFileInfo  `json:"info"`
	Entries []hostFileInfo `json:"entries"`
	Path    string         `json:"path"`
}

func decodeHost(value js.Value, target any) error {
	if value.IsNull() || value.IsUndefined() {
		return fs.ErrNotExist
	}
	return json.Unmarshal([]byte(js.Global().Get("JSON").Call("stringify", value).String()), target)
}

func (h hostFilesystem) operation(operation, path string) (filesystemResponse, error) {
	uri := h.workspace.URI(path)
	if uri == "" {
		return filesystemResponse{}, &fs.PathError{Op: operation, Path: path, Err: fs.ErrNotExist}
	}
	value, err := hostRequest(h.ctx, "actionlint/filesystem", struct {
		Operation string `json:"operation"`
		URI       string `json:"uri"`
	}{operation, uri})
	if err != nil {
		return filesystemResponse{}, err
	}
	var response filesystemResponse
	if err := decodeHost(value, &response); err != nil {
		return response, &fs.PathError{Op: operation, Path: path, Err: err}
	}
	return response, nil
}

func (h hostFilesystem) ReadFile(path string) ([]byte, error) {
	uri := h.workspace.URI(path)
	if uri == "" {
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrPermission}
	}
	value, err := hostRequest(h.ctx, "actionlint/readFile", struct {
		URI string `json:"uri"`
	}{uri})
	if err != nil {
		return nil, err
	}
	if value.IsNull() || value.IsUndefined() {
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrNotExist}
	}
	if value.Type() != js.TypeString {
		return nil, errors.New("workspace file response must be text or null")
	}
	return []byte(value.String()), nil
}

func (h hostFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	r, err := h.operation("readDirectory", path)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, 0, len(r.Entries))
	for _, info := range r.Entries {
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (h hostFilesystem) info(operation, path string) (fs.FileInfo, error) {
	r, err := h.operation(operation, path)
	if err != nil {
		return nil, err
	}
	if r.Info == nil {
		return nil, errors.New("filesystem response contains no file metadata")
	}
	return *r.Info, nil
}
func (h hostFilesystem) Stat(path string) (fs.FileInfo, error)  { return h.info("stat", path) }
func (h hostFilesystem) Lstat(path string) (fs.FileInfo, error) { return h.info("lstat", path) }
func (h hostFilesystem) EvalSymlinks(path string) (string, error) {
	r, err := h.operation("realpath", path)
	if err != nil {
		return "", err
	}
	return h.workspace.Path(r.Path)
}

func (h hostFilesystem) Glob(path string) ([]string, error) {
	matches, err := fs.Glob(discoveryGlobFilesystem{host: h}, strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}
	for index := range matches {
		matches[index] = "/" + matches[index]
	}
	return matches, nil
}

// fs.Glob uses StatFS and ReadDirFS; discovery never opens file contents here.
type discoveryGlobFilesystem struct{ host hostFilesystem }

func (discoveryGlobFilesystem) Open(string) (fs.File, error) { return nil, fs.ErrInvalid }
func (g discoveryGlobFilesystem) Stat(path string) (fs.FileInfo, error) {
	return g.host.Stat("/" + path)
}

func (g discoveryGlobFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return g.host.ReadDir("/" + path)
}
func (h hostFilesystem) Platform() string { return h.platform }

type hostGitExit int

func (e hostGitExit) Error() string { return fmt.Sprintf("git exited with status %d", e) }
func (e hostGitExit) ExitCode() int { return int(e) }
func (h hostFilesystem) queryGit(ctx context.Context, root string, args ...string) (gitcommand.Output, error) {
	value, err := hostRequest(ctx, "actionlint/gitQuery", struct {
		URI  string   `json:"uri"`
		Args []string `json:"args"`
	}{h.workspace.URI(root), args})
	if err != nil {
		return gitcommand.Output{}, err
	}
	var response struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
		Path     string `json:"path"`
	}
	if err := decodeHost(value, &response); err != nil {
		return gitcommand.Output{}, err
	}
	if response.Path != "" {
		if path, err := h.workspace.Path(response.Path); err == nil {
			response.Stdout = path + "\n"
		} else if len(args) > 1 && args[1] == "--show-toplevel" {
			return gitcommand.Output{}, err
		}
	}
	output := gitcommand.Output{Stdout: []byte(response.Stdout), Stderr: []byte(response.Stderr)}
	if response.ExitCode != 0 {
		return output, hostGitExit(response.ExitCode)
	}
	return output, nil
}

func (h hostFilesystem) discover(ctx context.Context, uris []string, noIgnore bool) ([]string, error) {
	paths := make([]string, 0, len(uris))
	for _, uri := range uris {
		path, err := h.workspace.Path(uri)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	h.ctx = ctx
	selected, err := workspace.FindFilesWithFilesystem(ctx, paths, noIgnore, h, h.queryGit)
	if err != nil {
		return nil, err
	}
	found := make([]string, 0, len(selected.Files))
	for _, file := range selected.Files {
		found = append(found, h.workspace.URI(file.Path))
	}
	return found, nil
}
