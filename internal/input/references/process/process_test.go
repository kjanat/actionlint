package process

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

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
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
	var request references.Request
	if json.Unmarshal(data, &request) != nil {
		os.Exit(5)
	}
	if mode == "zero-repository-id" {
		fmt.Printf(`{"version":1,"repository_id":0,"repository":%q,"commit":%q,"observed_at":"2026-10-16T00:00:00Z","fetch_log":{}}`, request.Repository, strings.Repeat("a", 40))
		os.Exit(0)
	}
	repositoryID, err := repositoryid.Known(42)
	if err != nil {
		os.Exit(9)
	}
	if mode == "no-repository-id" {
		repositoryID = repositoryid.ID{}
	}
	result := references.Result{Version: 1, Repository: request.Repository, RepositoryID: repositoryID, Commit: strings.Repeat("a", 40), ObservedAt: "2026-10-16T00:00:00Z", FetchLog: json.RawMessage(`{}`)}
	if mode == "wrong-repository" {
		result.Repository = "other/repository"
	}
	if mode == "tree-and-commit" {
		result.Tree = strings.Repeat("b", 40)
	}
	if mode == "network" {
		result.Network = &references.RequestReport{AllRequestsReported: true, Requests: []references.RequestCounts{{Host: "github.com", RequestKind: "git", Credentials: "unknown", Attempts: 2}}}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(6)
	}
	os.Exit(0)
}

func TestProcessNetworkReceiptIsNotRecountedFromCache(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, session := accounting.TrackRequests(t.Context())
	p := NewProcess(executable, []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", "network"}, 10*time.Second)
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	first, err := p.Resolve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	first.Network.Requests[0].Attempts = 999
	second, err := p.Resolve(ctx, request)
	if err != nil || second.Network.Requests[0].Attempts != 2 {
		t.Fatalf("cached receipt aliased caller: %+v %v", second, err)
	}
	var attempts, cached uint64
	for _, group := range session.Statistics().Requests {
		attempts += group.Attempts
		cached += group.CacheHits
	}
	if attempts != 2 || cached != 1 {
		t.Fatalf("attempts=%d cache=%d", attempts, cached)
	}
}

func TestCommitishResolverEmitsAnEmptyReceiptBeforeTransport(t *testing.T) {
	executable := os.Getenv("ACTIONLINT_TEST_COMMITISH_RESOLVER")
	if executable == "" {
		t.Skip("set ACTIONLINT_TEST_COMMITISH_RESOLVER to the built commitish resolver")
	}
	ctx, session := accounting.TrackRequests(t.Context())
	provider := NewProcess(executable, nil, 10*time.Second)
	result, err := provider.Resolve(ctx, references.Request{Version: 1, Repository: "../invalid", Ref: "master", Kind: "action"})
	if err == nil || result.Network == nil || !result.Network.AllRequestsReported || len(result.Network.Requests) != 0 {
		t.Fatalf("invalid repository did not produce a complete zero-attempt receipt: result=%+v error=%v", result, err)
	}
	report := session.Statistics()
	if len(report.Requests) != 1 || report.Requests[0].ResolverCalls != 1 || report.Requests[0].UnreportedResolverCalls != 0 || report.Requests[0].Attempts != 0 {
		t.Fatalf("host did not consume the resolver receipt: %+v", report)
	}
}

func TestProcessCoalescesConcurrentRequests(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	p := NewProcess(executable, []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", "count:" + directory}, 10*time.Second)
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	const callers = 8
	start := make(chan struct{})
	results := make(chan references.Result, callers)
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
		result.FetchLog[0] = '['
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("provider invocations=%d, want 1; error=%v", len(entries)-1, err)
	}
	result, err := p.Resolve(t.Context(), request)
	if err != nil || string(result.FetchLog) != "{}" {
		t.Fatalf("caller mutated the cached fetch log: result=%+v error=%v", result, err)
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
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "main", Kind: "action"}
	for _, mode := range []string{"ok", "no-repository-id", "zero-repository-id", "wrong-repository", "tree-and-commit", "oversize", "sleep"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 10 * time.Second
			if mode == "sleep" {
				timeout = 30 * time.Millisecond
			}
			p := NewProcess(executable, []string{"-test.run=^TestResolverProcessHelper$", "--", "--resolver-helper", mode}, timeout)
			result, err := p.Resolve(t.Context(), request)
			if (err == nil) != (mode == "ok" || mode == "no-repository-id") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if err == nil && result.RepositoryID.IsZero() != (mode == "no-repository-id") {
				t.Fatalf("repository ID = %+v in mode %s", result.RepositoryID, mode)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := p.Resolve(ctx, request); err == nil {
				t.Fatal("ignored cancellation")
			}
		})
	}
}
