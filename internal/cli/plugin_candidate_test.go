package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func candidateCatalogue(t *testing.T) (string, string, string, *testutil.PluginRegistry) {
	t.Helper()
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewPluginRegistry(t)
	data := r.Add(t, "v1.0.0", wasm)
	var record struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	if err := json.Unmarshal(r.File(r.RecordURL("v1.0.0")), &record); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files := map[string][]byte{
		"index.json":                       []byte(`{"schema_version":1,"source":"candidate","plugins":[{"name":"github.com/publisher/plugin","plugin_id":"no-sudo","manifest":"plugins/no-sudo/index.json"}]}`),
		"plugins/no-sudo/index.json":       []byte(`{"source":"candidate","name":"github.com/publisher/plugin","plugin_id":"no-sudo","versions":[{"version":"v1.0.0","record":"versions/v1.json"}]}`),
		"plugins/no-sudo/versions/v1.json": fmt.Appendf(nil, `{"source":"candidate","artifact":%s,"url":"artifacts/plugin.wasm"}`, record.Artifact),
		"artifacts/plugin.wasm":            data,
	}
	for name, data := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(root) + "/"}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return root, u.String(), fmt.Sprintf("%x", sha256.Sum256(data)), r
}

func candidateProject(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// Local installation must create the normal project files and execute the exact
// imported component after staging disappears. Deleting its cache must then produce
// a plugin error. The network recorder catches accidental requests throughout.
func TestCandidateCLIInstallsAndExecutesOffline(t *testing.T) {
	root, registry, digest, network := candidateCatalogue(t)
	candidateProject(t)
	pluginCommandTest(t, 0, "plugin", "search", "sudo", "--registry", registry)
	pluginCommandTest(t, 0, "plugin", "versions", "publisher/plugin", "--registry", registry)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(".github", pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0].Origin != "candidate" || lock.Lock.Plugins[0].SHA256 != digest || lock.Lock.Plugins[0].OwnerID != "" || lock.Lock.Plugins[0].RepositoryID != "" {
		t.Fatalf("wrong candidate lock: %+v", lock.Lock)
	}
	for _, change := range []func(*pluginregistry.Pin){
		func(p *pluginregistry.Pin) { p.Registry = "https://example.com/" },
		func(p *pluginregistry.Pin) { p.Origin = "" },
		func(p *pluginregistry.Pin) { p.RepositoryID = "12" },
		func(p *pluginregistry.Pin) { p.OwnerID = "34" },
	} {
		pin := lock.Lock.Plugins[0]
		change(&pin)
		if err := pin.Validate(); err == nil {
			t.Fatalf("mixed candidate authority accepted: %+v", pin)
		}
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install")
	if output := pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml"); !strings.Contains(output, "[no-sudo]") {
		t.Fatal(output)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cache, "actionlint", "artifacts", digest)); err != nil {
		t.Fatal(err)
	}
	selected, err := (localapp.PluginSelection{}).SelectForCache(t.Context(), ".", application.Input{Kind: application.Files, Paths: []string{"workflow.yml"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.CachePlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Directory != filepath.Join(cache, "actionlint", "artifacts") || len(plan.Artifacts) != 1 || plan.Artifacts[0] != digest {
		t.Fatalf("candidate missing from restore plan: %+v", plan)
	}
	pluginCommandTest(t, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	beforeSync, err := os.ReadFile(lock.Path)
	if err != nil {
		t.Fatal(err)
	}
	if output := pluginCommandTest(t, 3, "plugin", "sync"); !strings.Contains(output, "publisher/plugin@v1.0.0") {
		t.Fatalf("missing staging error lost the plugin identity: %s", output)
	}
	afterSync, err := os.ReadFile(lock.Path)
	if err != nil || !bytes.Equal(afterSync, beforeSync) {
		t.Fatalf("failed sync changed the installed lock: %v", err)
	}
	if len(network.Requests()) != 0 {
		t.Fatalf("local candidate contacted network: %v", network.Requests())
	}
}

// Authors rebuild candidates without consuming release versions. An explicit
// update must select the rebuilt bytes, while ordinary checks keep executing the
// earlier imported bytes until the author asks for that update.
func TestCandidateCLIUpdatesSameVersion(t *testing.T) {
	root, registry, digest, _ := candidateCatalogue(t)
	candidateProject(t)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	file := filepath.Join(root, "artifacts", "plugin.wasm")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(data, []byte("Test plugin"), []byte("Next plugin"), 1)
	if bytes.Equal(data, changed) {
		t.Fatal("candidate rebuild did not change bytes")
	}
	if err := os.WriteFile(file, changed, 0600); err != nil {
		t.Fatal(err)
	}
	next := fmt.Sprintf("%x", sha256.Sum256(changed))
	recordPath := filepath.Join(root, "plugins", "no-sudo", "versions", "v1.json")
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	record = bytes.ReplaceAll(record, []byte(digest), []byte(next))
	if err := os.WriteFile(recordPath, record, 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	lockPath := filepath.Join(".github", pluginregistry.LockName)
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Lock.Plugins[0].SHA256 != digest {
		t.Fatal("checking updated candidate lock")
	}
	pluginCommandTest(t, 0, "plugin", "update", "publisher/plugin")
	lock, err = pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Lock.Plugins[0].SHA256 != next {
		t.Fatal("explicit update did not import rebuilt candidate")
	}
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cache, "actionlint", "artifacts", next)); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	pluginCommandTest(t, 0, "plugin", "sync")
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
}

// A broken rebuild must leave an installed candidate usable. Repairing its
// declaration should then allow an explicit update without resetting the project.
func TestCandidateCLIFailedUpdateKeepsInstalledBytes(t *testing.T) {
	root, registry, digest, network := candidateCatalogue(t)
	candidateProject(t)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	configPath := filepath.Join(".github", "actionlint.yaml")
	lockPath := filepath.Join(".github", pluginregistry.LockName)
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "artifacts", "plugin.wasm")
	previous, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := bytes.Replace(previous, []byte("Test plugin"), []byte("Next plugin"), 1)
	if bytes.Equal(previous, rebuilt) {
		t.Fatal("rebuild did not change the candidate")
	}
	if err := os.WriteFile(file, rebuilt, 0600); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(root, "plugins", "no-sudo", "versions", "v1.json")
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := strings.Repeat("b", 64)
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(digest), []byte(wrongDigest)), 0600); err != nil {
		t.Fatal(err)
	}
	output := pluginCommandTest(t, 3, "plugin", "update", "publisher/plugin")
	if !strings.Contains(output, "SHA256 mismatch") {
		t.Fatalf("wrong candidate failure: %s", output)
	}
	for path, before := range map[string][]byte{configPath: beforeConfig, lockPath: beforeLock} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("failed update changed %s: %v", path, err)
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cached, err := os.ReadFile(filepath.Join(cache, "actionlint", "artifacts", digest))
	if err != nil || !bytes.Equal(cached, previous) {
		t.Fatalf("failed update changed installed bytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "actionlint", "artifacts", wrongDigest)); !os.IsNotExist(err) {
		t.Fatalf("failed update cached unverified bytes: %v", err)
	}
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	next := fmt.Sprintf("%x", sha256.Sum256(rebuilt))
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(digest), []byte(next)), 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "update", "publisher/plugin")
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0].SHA256 != next {
		t.Fatalf("repaired rebuild did not update lock: %+v, %v", lock, err)
	}
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if len(network.Requests()) != 0 {
		t.Fatalf("candidate update contacted the registry: %v", network.Requests())
	}
}

// A syntactically relative path can still leave the catalogue through a symlink.
// The target contains the declared bytes with the correct digest. Only the path
// escape should cause rejection, before either project file is written.
func TestCandidateCLIRejectsArtifactSymlinkEscape(t *testing.T) {
	root, registry, _, _ := candidateCatalogue(t)
	file := filepath.Join(root, "artifacts", "plugin.wasm")
	outside := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.Rename(file, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	candidateProject(t)
	pluginCommandTest(t, 3, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	if _, err := os.Stat(filepath.Join(".github", pluginregistry.LockName)); !os.IsNotExist(err) {
		t.Fatalf("escaping candidate wrote lock: %v", err)
	}
}

// A candidate catalogue is untrusted input, even when explicitly selected.
// Malformed identities, forged publication fields and escaping paths must fail
// before installation writes either the project declaration or lockfile.
func TestCandidateCLIRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct{ name, file, before, after string }{
		{"tampered bytes", "artifacts/plugin.wasm", "Test plugin", "Fake plugin"},
		{"artifact parent", "plugins/no-sudo/versions/v1.json", "artifacts/plugin.wasm", "../plugin.wasm"},
		{"artifact absolute", "plugins/no-sudo/versions/v1.json", "artifacts/plugin.wasm", "/plugin.wasm"},
		{"artifact URL", "plugins/no-sudo/versions/v1.json", "artifacts/plugin.wasm", "https://example.com/plugin.wasm"},
		{"manifest parent", "index.json", "plugins/no-sudo/index.json", "../index.json"},
		{"record parent", "plugins/no-sudo/index.json", "versions/v1.json", "versions/../../v1.json"},
		{"forged authority", "plugins/no-sudo/versions/v1.json", `"source":"candidate"`, `"source":"candidate","receipt":{}`},
		{"wrong metadata", "plugins/no-sudo/versions/v1.json", `"plugin_id":"no-sudo"`, `"plugin_id":"impostor"`},
		{"published catalogue", "index.json", `"source":"candidate"`, `"source":"release"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, registry, _, _ := candidateCatalogue(t)
			file := filepath.Join(root, filepath.FromSlash(tc.file))
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(data, []byte(tc.before), []byte(tc.after), 1)
			if bytes.Equal(data, changed) {
				t.Fatal("mutation did not change fixture")
			}
			if err := os.WriteFile(file, changed, 0600); err != nil {
				t.Fatal(err)
			}
			candidateProject(t)
			pluginCommandTest(t, 3, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
			for _, name := range []string{pluginregistry.LockName, "actionlint.yaml"} {
				if _, err := os.Stat(filepath.Join(".github", name)); !os.IsNotExist(err) {
					t.Fatalf("failed candidate wrote %s: %v", name, err)
				}
			}
		})
	}
}
