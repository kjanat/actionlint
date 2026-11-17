// Package localproviders reads tool provider manifests that a configuration
// names by a local path, together with the transform programs beside them.
package localproviders

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// Read reads a process provider manifest from path and the transform
// programs it references. A local manifest runs its entry as found on PATH,
// so it declares no artifacts; transforms it names by digest are the *.jq
// files beside it with that content.
func Read(reader *files.Reader, path string) (toolmanifest.Manifest, toolmanifest.Programs, error) {
	data, err := reader.ReadFile(path)
	if err != nil {
		return toolmanifest.Manifest{}, nil, err
	}
	manifest, err := toolmanifest.ParsePluginManifest(data)
	if err != nil {
		return toolmanifest.Manifest{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	if manifest.Transport != toolmanifest.TransportProcess {
		return toolmanifest.Manifest{}, nil, fmt.Errorf("%s: a local tool provider manifest must use the process transport, not %q", path, manifest.Transport)
	}
	if len(manifest.Artifacts) != 0 {
		return toolmanifest.Manifest{}, nil, fmt.Errorf("%s: a local tool provider manifest runs a command from PATH and declares no artifacts", path)
	}
	programs := map[string][]byte{}
	resolve := func(digest string) ([]byte, bool) {
		program, ok := programs[digest]
		return bytes.Clone(program), ok
	}
	var wanted []string
	for _, transform := range manifest.Transforms() {
		if transform.Digest != "" {
			wanted = append(wanted, transform.Digest)
		}
	}
	if len(wanted) == 0 {
		return manifest, resolve, nil
	}
	directory := filepath.Dir(path)
	entries, err := reader.ReadDir(directory)
	if err != nil {
		return toolmanifest.Manifest{}, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jq") {
			continue
		}
		program, err := reader.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return toolmanifest.Manifest{}, nil, err
		}
		programs["sha256:"+sha256hex.Digest(program)] = program
	}
	var missing []string
	for _, digest := range wanted {
		if _, ok := programs[digest]; !ok {
			missing = append(missing, digest)
		}
	}
	if len(missing) != 0 {
		return toolmanifest.Manifest{}, nil, fmt.Errorf("%s: %w: %s", path, errMissingProgram, strings.Join(missing, ", "))
	}
	return manifest, resolve, nil
}

// errMissingProgram reports a transform digest no file beside the manifest has.
var errMissingProgram = errors.New("no .jq file beside the manifest has the transform digest")
