package remotescan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"
const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n"

func options(server string) Options {
	return Defaults(Options{Repository: "owner/repo", Ref: testSHA, ServerURL: server, APIURL: server + "/api/v3"}, func(string) string { return "" })
}

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	zipped := gzip.NewWriter(&output)
	tarred := tar.NewWriter(zipped)
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		content := files[name]
		if err := tarred.WriteHeader(&tar.Header{Name: "repo-" + testSHA + "/" + name, Mode: 0600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarred, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarred.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestDefaults(t *testing.T) {
	env := map[string]string{"GITHUB_REPOSITORY": "owner/repo", "GITHUB_SHA": testSHA, "GITHUB_REF": "refs/heads/main", "GITHUB_SERVER_URL": "https://enterprise.example", "GITHUB_API_URL": "https://enterprise.example/api/v3", "GITHUB_GRAPHQL_URL": "https://unrelated.example/graphql", "GITHUB_TOKEN": "private"}
	getenv := func(key string) string { return env[key] }
	got := Defaults(Options{}, getenv)
	if got.Ref != testSHA || got.APIURL != env["GITHUB_API_URL"] || got.Token != "private" {
		t.Fatalf("incorrect Actions defaults: %+v", got)
	}
	got = Defaults(Options{Repository: "other/repo", ServerURL: "https://github.com"}, getenv)
	if got.Ref != "" || got.APIURL != "https://api.github.com" {
		t.Fatalf("foreign repository inherited Actions ref/API: %+v", got)
	}
	got = Defaults(Options{Ref: "release"}, getenv)
	if got.Ref != "release" {
		t.Fatal("explicit ref lost")
	}
	got = Defaults(Options{ServerURL: "https://github.com/"}, getenv)
	if got.Ref != "" || got.APIURL != "https://api.github.com" {
		t.Fatalf("foreign server inherited Actions ref/API: %+v", got)
	}
}

func TestArchiveScanResolvesOnceAndUsesProject(t *testing.T) {
	data := archive(t, map[string]string{
		".github/workflows/main.yaml": workflow + "      - uses: ./local\n        with:\n          nonexistent: value\n",
		".github/actionlint.yaml":     "policy:\n  require-commit-hash: true\n",
		"local/action.yml":            "name: local\ndescription: local\ninputs:\n  known:\n    description: known\nruns:\n  using: composite\n  steps:\n    - run: echo safe\n      shell: bash\n",
	})
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing expected auth")
		}
		switch r.URL.Path {
		case "/api/v3/repos/owner/repo/commits/release":
			fmt.Fprintf(w, `{"sha":%q}`, testSHA)
		case "/api/v3/repos/owner/repo/tarball/" + testSHA:
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/owner/repo/archive/" + testSHA + ".tar.gz":
			_, _ = w.Write(data)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	opts := options(server.URL)
	opts.Ref = "release"
	opts.Token = "test-secret"
	result, err := scan(context.Background(), opts, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.SHA != testSHA || result.Source.Transport != "server-archive" || len(paths) != 3 {
		t.Fatalf("incorrect pinned fallback: %+v, %v", result, paths)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return strings.Contains(d.Message, "commit") }) {
		t.Fatalf("repository policy not applied: %+v", result.Diagnostics)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return strings.Contains(d.Message, "nonexistent") }) {
		t.Fatalf("local action not resolved: %+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if !strings.Contains(diagnostic.URL, "/blob/"+testSHA+"/") || filepath.IsAbs(diagnostic.Path) {
			t.Fatalf("bad source location: %+v", diagnostic)
		}
	}
	var jsonOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jsonOutput.String(), opts.Token) {
		t.Fatal("credential leaked")
	}
}

func TestRedirectStripsCredentials(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("cross-host credential leak")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	s := scanner{opts: options(origin.URL), client: origin.Client()}
	s.opts.Token = "private"
	response, err := s.request(context.Background(), origin.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
		size int64
	}{
		{"../escape", tar.TypeReg, 0}, {"root/../../escape", tar.TypeReg, 0}, {"root/C:/file", tar.TypeReg, 0}, {"root/a\\b", tar.TypeReg, 0}, {"root/.git/config", tar.TypeReg, 0}, {"root/NUL", tar.TypeReg, 0}, {"root/link", tar.TypeSymlink, 0}, {"root/link", tar.TypeLink, 0}, {"root/file", tar.TypeReg, 20},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.name, tc.kind), func(t *testing.T) {
			var b bytes.Buffer
			tw := tar.NewWriter(&b)
			if err := tw.WriteHeader(&tar.Header{Name: tc.name, Typeflag: tc.kind, Mode: 0600, Size: tc.size, Linkname: "outside"}); err != nil {
				t.Fatal(err)
			}
			_, _ = tw.Write(make([]byte, tc.size))
			_ = tw.Close()
			opts := options("https://github.com")
			opts.MaxFileBytes = 10
			if err := extract(context.Background(), &b, t.TempDir(), opts); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestInvalidArchiveStopsFallbackAndCleansUp(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, "not an archive") }))
	defer server.Close()
	result, err := scan(context.Background(), options(server.URL), server.Client())
	if err == nil || calls != 1 || result.Error == "" {
		t.Fatalf("invalid archive fallback: %+v %v, requests %d", result, err, calls)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary snapshots remain: %v %v", entries, err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := scan(ctx, options("https://github.com"), &http.Client{})
	if err == nil || len(result.Attempts) != 0 {
		t.Fatalf("cancelled scan proceeded: %+v, %v", result, err)
	}
}

func fixtureGit(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git: %v: %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--template=", "--initial-branch=main")
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "workflows", "test.yml"), []byte(workflow), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(".github export-ignore\n*.yml export-subst\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "Fixture")
	return dir, git("rev-parse", "HEAD")
}

func TestLocalCommittedSnapshotIgnoresAttributesAndDirtyFiles(t *testing.T) {
	dir, sha := fixtureGit(t)
	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/owner/repo.git")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "workflows", "test.yml"), []byte("dirty: invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := options("https://github.com")
	opts.APIURL = "https://api.github.com"
	opts.Local = dir
	opts.Ref = "main"
	result, err := Scan(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.SHA != sha || result.Source.Transport != "local" || len(result.Diagnostics) != 0 {
		t.Fatalf("incorrect committed snapshot: %+v", result)
	}
}

func TestHTTPSGitFallback(t *testing.T) {
	dir, sha := fixtureGit(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		args := []string{"upload-pack", "--stateless-rpc"}
		switch r.URL.Path {
		case "/owner/repo.git/info/refs":
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000")
			args = append(args, "--advertise-refs")
		case "/owner/repo.git/git-upload-pack":
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		cmd := exec.Command("git", append(args, dir)...)
		cmd.Stdin = r.Body
		cmd.Stdout = w
		if err := cmd.Run(); err != nil {
			t.Errorf("fixture upload-pack: %v", err)
		}
	}))
	defer server.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	certFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certFile, cert, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", certFile)
	opts := options(server.URL)
	opts.Ref = sha
	opts.Timeout = 8 * time.Second
	result, err := scan(context.Background(), opts, server.Client())
	if err != nil {
		t.Fatalf("Git fallback: %v %+v", err, result.Attempts)
	}
	if result.Source.SHA != sha || result.Source.Transport != "git-https" {
		t.Fatalf("incorrect Git source: %+v", result)
	}
}

func TestGitCredentialsOnlyInScopedEnvironment(t *testing.T) {
	s := scanner{opts: options("https://github.com"), temp: t.TempDir()}
	s.opts.Token = "secret"
	cmd := s.gitCommand(context.Background(), s.temp, true, "fetch", "https://github.com/owner/repo.git", testSHA)
	if strings.Contains(strings.Join(cmd.Args, " "), s.opts.Token) {
		t.Fatal("token in arguments")
	}
	if !slices.Contains(cmd.Env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraHeader") {
		t.Fatal("token header not scoped to selected host")
	}
}

func TestSnapshotRejectsDuplicateAndCaseAliasedPaths(t *testing.T) {
	for _, names := range [][]string{{"a", "a"}, {"A", "a"}, {"A/x", "a/y"}, {"a", "a/x"}} {
		writer := snapshotWriter{root: t.TempDir(), opts: options("https://github.com"), names: map[string]string{}}
		if err := writer.write(context.Background(), names[0], 1, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
		if err := writer.write(context.Background(), names[1], 1, strings.NewReader("y")); err == nil {
			t.Fatalf("accepted conflicting paths %v", names)
		}
	}
}

func TestMalformedCommitResponseStopsFallback(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = io.WriteString(w, `{"sha":"main"}`) }))
	defer server.Close()
	opts := options(server.URL)
	opts.Ref = "main"
	result, err := scan(context.Background(), opts, server.Client())
	if err == nil || calls != 1 || result.Source.SHA != "" {
		t.Fatalf("invalid commit response accepted: %+v, %v", result, err)
	}
}

func TestUnavailableLocalRefDoesNotSelectHEAD(t *testing.T) {
	dir, _ := fixtureGit(t)
	s := scanner{opts: options("https://github.com"), temp: t.TempDir()}
	sha, err := s.resolveLocal(context.Background(), dir, "missing")
	if err == nil || sha != "" {
		t.Fatalf("missing ref resolved to %s: %v", sha, err)
	}
}

func TestGitCommandCancellation(t *testing.T) {
	if os.Getenv("ACTIONLINT_SCAN_CANCEL_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s := scanner{opts: options("https://github.com"), temp: t.TempDir()}
	cmd := s.gitCommand(ctx, s.temp, false)
	cmd.Path = os.Args[0]
	cmd.Args = []string{os.Args[0], "-test.run=^TestGitCommandCancellation$"}
	cmd.Env = append(cmd.Env, "ACTIONLINT_SCAN_CANCEL_HELPER=1")
	start := time.Now()
	if err := cmd.Run(); err == nil {
		t.Fatal("cancelled child completed successfully")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled Git child was not terminated promptly")
	}
}
