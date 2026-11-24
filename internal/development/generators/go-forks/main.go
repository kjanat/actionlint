// Command go-forks sets the version labels in docs/go-forks.md to the versions Go selects.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const document = "docs/go-forks.md"

var (
	modules = []string{".", "plugins/sdk/go", "plugins/examples/insecure-download"}

	// Renovate's custom manager in renovate.json matches the same marker and label.
	markerPattern = regexp.MustCompile(`^\s*%% renovate: depName=(\S+)\s*$`)
	labelPattern  = regexp.MustCompile(`<br/>(v[0-9][0-9A-Za-z.+-]*)`)
)

func main() {
	check := flag.Bool("check", false, "fail when "+document+" differs from the selected versions")
	flag.Parse()
	if err := run(context.Background(), ".", *check, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, root string, check bool, stdout io.Writer) error {
	path := filepath.Join(root, document)
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	labels, err := parse(current)
	if err != nil {
		return fmt.Errorf("%s: %w", document, err)
	}
	paths := make([]string, 0, len(labels))
	for _, label := range labels {
		paths = append(paths, label.module)
	}
	selections := map[string][]selection{}
	for _, dir := range modules {
		versions, err := selected(ctx, filepath.Join(root, dir), paths)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		for module, version := range versions {
			selections[module] = append(selections[module], selection{dir, version})
		}
	}
	updated, changes, err := rewrite(current, labels, selections)
	if err != nil {
		return fmt.Errorf("%s: %w", document, err)
	}
	for _, change := range changes {
		fmt.Fprintf(stdout, "%s: %s %s -> %s\n", document, change.module, change.from, change.to)
	}
	if len(changes) == 0 {
		return nil
	}
	if check {
		return fmt.Errorf("%s has stale version labels; run mise run generate:go-forks", document)
	}
	return os.WriteFile(path, updated, 0o644)
}

type label struct {
	module string
	line   int
	start  int
	end    int
}

type selection struct {
	dir     string
	version string
}

type change struct {
	module string
	from   string
	to     string
}

func parse(doc []byte) ([]label, error) {
	lines := bytes.SplitAfter(doc, []byte("\n"))
	var labels []label
	for i, line := range lines {
		marker := markerPattern.FindSubmatch(bytes.TrimRight(line, "\r\n"))
		if marker == nil {
			continue
		}
		module := string(marker[1])
		if i+1 == len(lines) {
			return nil, fmt.Errorf("line %d: %s has no label line", i+1, module)
		}
		version := labelPattern.FindSubmatchIndex(lines[i+1])
		if version == nil {
			return nil, fmt.Errorf("line %d: %s has no <br/>v... version label", i+2, module)
		}
		labels = append(labels, label{module, i + 1, version[2], version[3]})
	}
	if len(labels) == 0 {
		return nil, errors.New("no renovate depName markers")
	}
	return labels, nil
}

func rewrite(doc []byte, labels []label, selections map[string][]selection) ([]byte, []change, error) {
	lines := bytes.SplitAfter(doc, []byte("\n"))
	var changes []change
	for _, label := range labels {
		found := selections[label.module]
		if len(found) == 0 {
			return nil, nil, fmt.Errorf("%s is in no module graph of %s", label.module, strings.Join(modules, ", "))
		}
		want := found[0].version
		for _, other := range found[1:] {
			if other.version != want {
				return nil, nil, fmt.Errorf("%s is %s in %s and %s in %s", label.module, want, found[0].dir, other.version, other.dir)
			}
		}
		line := lines[label.line]
		have := string(line[label.start:label.end])
		if have == want {
			continue
		}
		lines[label.line] = slices.Concat(line[:label.start], []byte(want), line[label.end:])
		changes = append(changes, change{label.module, have, want})
	}
	return bytes.Join(lines, nil), changes, nil
}

type listed struct {
	Path    string
	Version string
	Error   *struct{ Err string }
}

func selected(ctx context.Context, dir string, paths []string) (map[string]string, error) {
	command := exec.CommandContext(ctx, "go", append([]string{"list", "-m", "-e", "-json"}, paths...)...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -m: %w: %s", err, &stderr)
	}
	versions := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var module listed
		if err := decoder.Decode(&module); errors.Is(err, io.EOF) {
			return versions, nil
		} else if err != nil {
			return nil, fmt.Errorf("go list -m: %w", err)
		}
		if module.Version != "" {
			versions[module.Path] = module.Version
		} else if module.Error == nil {
			return nil, fmt.Errorf("go list -m: %s has no version", module.Path)
		}
	}
}
