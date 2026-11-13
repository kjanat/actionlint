package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Local metadata queries must finish without waiting for network services or
// interactive input. A caller's earlier deadline takes precedence.
const queryTimeout = 10 * time.Second

func Command(ctx context.Context, git, root string, args ...string) *exec.Cmd {
	// Index inspection must not invoke repository-configured fsmonitor commands.
	command := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false"}, args...)...)
	// A descendant can keep stdout or stderr open after Git exits or is killed.
	command.WaitDelay = 5 * time.Second
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

// Output contains a Git query's separate output streams, including warnings
// written to stderr when Git exits successfully.
type Output struct {
	Stdout []byte
	Stderr []byte
}

// Query runs one local Git query with a deadline and bounded pipe draining.
// A nil environment uses Command's repository environment; a supplied environment
// replaces it so discovery can preserve its filesystem and pathspec settings.
func Query(ctx context.Context, git, root string, environment []string, args ...string) (Output, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	command := Command(ctx, git, root, args...)
	if environment != nil {
		command.Env = environment
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}
