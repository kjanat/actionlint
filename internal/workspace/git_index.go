package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/execabs"
)

// GitIndex is a read-only snapshot of the prospective tree and its watcher input.
type GitIndex struct {
	Modes map[string]string
	Path  string
}

func ReadIndex(ctx context.Context, root string) (result GitIndex, err error) {
	git, err := execabs.LookPath("git")
	if err != nil {
		return result, err
	}
	output, err := GitCommand(ctx, git, root, "ls-files", "--stage", "-z").Output()
	if err != nil {
		err = fmt.Errorf("read Git index: %w", err)
		return result, err
	}
	result.Modes, err = ParseIndexModes(string(output))
	if err != nil {
		return result, err
	}
	// Intent-to-add entries appear in ls-files, but are absent from the
	// prospective tree. Compare cached views so deleted worktree files are
	// handled too, without writing a tree or parsing Git's debug format.
	additions := make(map[string]struct{})
	for _, visibility := range []string{"--ita-visible-in-index", "--ita-invisible-in-index"} {
		output, err := GitCommand(ctx, git, root, "diff", "--cached", "--name-status", "-z",
			"--diff-filter=AD", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color",
			"--ignore-submodules=all", visibility, "--").Output()
		if err != nil {
			err = fmt.Errorf("read Git index additions: %w", err)
			return result, err
		}
		records := strings.Split(string(output), "\x00")
		if len(records)%2 != 1 || records[len(records)-1] != "" {
			err = fmt.Errorf("unexpected Git index changes %q", output)
			return result, err
		}
		for i := 0; i+1 < len(records); i += 2 {
			status, name := records[i], records[i+1]
			if visibility == "--ita-visible-in-index" {
				if status == "A" {
					additions[name] = struct{}{}
				}
			} else {
				delete(additions, name)
				if status == "D" {
					// A HEAD path re-added with -N is absent from the tree,
					// including when HEAD already contains an empty blob.
					delete(result.Modes, name)
				}
			}
		}
	}
	for name := range additions {
		delete(result.Modes, name)
	}
	index, err := GitCommand(ctx, git, root, "rev-parse", "--git-path", "index").Output()
	if err != nil {
		err = fmt.Errorf("locate Git index: %w", err)
		return result, err
	}
	location := strings.TrimSuffix(strings.TrimSuffix(string(index), "\n"), "\r")
	if location == "" || strings.ContainsAny(location, "\x00\r\n") {
		err = fmt.Errorf("unexpected Git index location %q", index)
		return result, err
	}
	if !filepath.IsAbs(location) {
		location = filepath.Join(root, location)
	}
	result.Path, err = filepath.Abs(location)
	return result, err
}

func GitCommand(ctx context.Context, git, root string, args ...string) *exec.Cmd {
	// Index inspection must not invoke repository-configured fsmonitor commands.
	command := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false"}, args...)...)
	// Clear repository overrides inherited from Git commands or hooks so the
	// query uses the workflow's project.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	return command
}

func ParseIndexModes(output string) (map[string]string, error) {
	modes := make(map[string]string)
	for record := range strings.SplitSeq(output, "\x00") {
		if record == "" {
			continue
		}
		metadata, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || name == "" {
			return nil, fmt.Errorf("unexpected Git index entry %q", record)
		}
		if fields[2] != "0" {
			modes[name] = "unmerged"
		} else if modes[name] != "unmerged" {
			modes[name] = fields[0]
		}
	}
	return modes, nil
}
