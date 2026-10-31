package resolution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/resolver"
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
	if directory, ok := strings.CutPrefix(mode, "count:"); ok {
		if err := os.WriteFile(filepath.Join(directory, strconv.Itoa(os.Getpid())), nil, 0o600); err != nil {
			os.Exit(7)
		}
		release := filepath.Join(directory, "release")
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(release); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(8)
			}
			time.Sleep(time.Millisecond)
		}
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

func TestProcessCoalescesConcurrentRequests(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	p := NewProcess(executable, []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", "count:" + directory}, 10*time.Second)
	request := resolver.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	const callers = 8
	start := make(chan struct{})
	results := make(chan resolver.Result, callers)
	failures := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			result, err := p.Resolve(t.Context(), request)
			results <- result
			failures <- err
		}()
	}
	ready.Wait()
	close(start)
	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resolver process did not start")
		}
		time.Sleep(time.Millisecond)
	}
	// Keep the first provider blocked while the simultaneous callers join it.
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Resolve(ctx, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting caller ignored its deadline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for range callers {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result.Commit != strings.Repeat("a", 40) {
			t.Fatalf("result=%+v", result)
		}
		result.Evidence[0] = '['
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("provider invocations=%d, want 1; error=%v", len(entries)-1, err)
	}
	result, err := p.Resolve(t.Context(), request)
	if err != nil || string(result.Evidence) != "{}" {
		t.Fatalf("caller mutated shared evidence: result=%+v error=%v", result, err)
	}
	if _, err := p.NewSession().Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(directory)
	if err != nil || len(entries) != 3 {
		t.Fatalf("new analysis reused a mutable-ref result: invocations=%d, error=%v", len(entries)-1, err)
	}
}

func TestProcessBoundsAndIdentity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	request := resolver.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	for _, mode := range []string{"ok", "wrong-repository", "tree-and-commit", "oversize", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 10 * time.Second
			if mode == "sleep" {
				timeout = 30 * time.Millisecond
			}
			p := NewProcess(executable, []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", mode}, timeout)
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
