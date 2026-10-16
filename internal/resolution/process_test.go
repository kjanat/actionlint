package resolution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/resolver"
)

func TestResolverProcessHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--resolver-helper" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "oversize" {
		fmt.Print(strings.Repeat("x", (2<<20)+1))
		os.Exit(0)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(4)
	}
	var request resolver.Request
	if json.Unmarshal(data, &request) != nil {
		os.Exit(5)
	}
	result := resolver.Result{Version: 1, Repository: request.Repository, RepositoryID: 42, Commit: strings.Repeat("a", 40), ObservedAt: "2026-10-16T00:00:00Z", Evidence: json.RawMessage(`{}`)}
	if mode == "wrong-repository" {
		result.Repository = "other/repository"
	}
	if mode == "tree-and-commit" {
		result.Tree = strings.Repeat("b", 40)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(6)
	}
	os.Exit(0)
}
func TestProcessBoundsAndIdentity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := resolver.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	for _, mode := range []string{"ok", "wrong-repository", "tree-and-commit", "oversize", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			p := &Process{Executable: executable, Arguments: []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", mode}, Timeout: 10 * time.Second}
			if mode == "sleep" {
				p.Timeout = 30 * time.Millisecond
			}
			result, err := p.Resolve(t.Context(), request)
			if (err == nil) != (mode == "ok") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := p.Resolve(ctx, request); err == nil {
				t.Fatal("ignored cancellation")
			}
		})
	}
}
