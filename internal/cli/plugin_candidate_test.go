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

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/ocitest"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func candidateCatalogue(t *testing.T) (string, string, string, *fixtureregistry.PluginRegistry) {
	t.Helper()
	wasm, err := os.ReadFile("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := fixtureregistry.NewPluginRegistry(t)
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

func candidateProject(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	candidateProjectAt(t, directory)
	return directory
}

func candidateProjectAt(t *testing.T, directory string) {
	t.Helper()
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "workflow.yml"), []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func candidateDigest(t *testing.T, path string) string {
	t.Helper()
	loaded, err := checking.ConfigurationRules().Read(path, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Config.Plugins) != 1 {
		t.Fatalf("config declares %d plugins, want one", len(loaded.Config.Plugins))
	}
	return loaded.Config.Plugins[0].CandidateDigest()
}

// Local installation must create the normal project files and execute the exact
// imported component after staging disappears. Deleting its cache must then produce
// a plugin error. The network recorder catches accidental requests throughout.
func TestCandidateCLIInstallsAndExecutesOffline(t *testing.T) {
	root, registry, digest, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
	t.Chdir(candidateProject(t))
	pluginCommandTest(t, acquisition, 0, "plugin", "search", "sudo", "--registry", registry)
	pluginCommandTest(t, acquisition, 0, "plugin", "versions", "publisher/plugin", "--registry", registry)
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	configPath := filepath.Join(".github", "actionlint.yaml")
	if got := candidateDigest(t, configPath); got != digest {
		t.Fatalf("candidate config digest = %q, want %q", got, digest)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("candidate install created a project lock: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install")
	if output := pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml"); !strings.Contains(output, "[no-sudo]") {
		t.Fatal(output)
	}
	cache := acquisition.UserCacheDir
	selected, err := (plugininstall.Selection{Acquisition: acquisition}).SelectForCache(t.Context(), ".", inputmodel.Request{Kind: inputmodel.Files, Paths: []string{"workflow.yml"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.CachePlan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Directory != filepath.Join(cache, "actionlint", "artifacts") || len(plan.Artifacts) != 1 || plan.Artifacts[0] != digest {
		t.Fatalf("candidate missing from restore plan: %+v", plan)
	}
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if err := os.Remove(filepath.Join(cache, "actionlint", "artifacts", digest)); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	beforeSync, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if output := pluginCommandTest(t, acquisition, 3, "plugin", "sync"); !strings.Contains(output, "publisher/plugin@v1.0.0") {
		t.Fatalf("missing staging error lost the plugin identity: %s", output)
	}
	afterSync, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(afterSync, beforeSync) {
		t.Fatalf("failed sync changed the candidate declaration: %v", err)
	}
	if len(network.Requests()) != 0 {
		t.Fatalf("local candidate contacted network: %v", network.Requests())
	}
}

// An OCI candidate binds the root digest through installation and normal checks.
// The fixture preloads the real OCI store, then stops the registry: installation,
// remembered selection and every check must work without acquisition authority.
func TestCandidateCLIOCIInstallsAndExecutesOffline(t *testing.T) {
	root, registry, previousDigest, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir()}
	t.Chdir(candidateProject(t))
	wasm, err := os.ReadFile(filepath.Join(root, "artifacts", "plugin.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	remote := ocitest.New(t)
	primary := remote.Blob(artifacts.MediaTypeWasm, wasm)
	manifest := remote.Manifest(t, primary)
	index := remote.Index(t, map[string]ocitest.Descriptor{artifacts.HostPlatform(): manifest})
	source := remote.Reference(index)
	options := artifacts.Options{CacheDir: filepath.Join(acquisition.UserCacheDir, "actionlint", "artifacts")}
	pluginCommandTest(t, acquisition, 0, "plugin", "inspect", source)
	remote.Close()
	recordPath := filepath.Join(root, "plugins", "no-sudo", "versions", "v1.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Source   string          `json:"source"`
		Artifact json.RawMessage `json:"artifact"`
		URL      string          `json:"url"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record.Artifact = bytes.ReplaceAll(record.Artifact, []byte(previousDigest), []byte(index.Hex()))
	record.URL = source
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	configPath := filepath.Join(".github", "actionlint.yaml")
	if got := candidateDigest(t, configPath); got != index.Hex() {
		t.Fatalf("candidate pinned %q, want OCI root %q", got, index.Hex())
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install")
	check := []string{"check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml"}
	if output := pluginCommandTest(t, acquisition, 1, check...); !strings.Contains(output, "[no-sudo]") {
		t.Fatalf("OCI candidate did not execute: %s", output)
	}
	for _, descriptor := range []ocitest.Descriptor{index, manifest, primary} {
		file := filepath.Join(options.CacheDir, descriptor.Hex())
		cached, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if output := pluginCommandTest(t, acquisition, 3, check...); !strings.Contains(output, "plugin") {
			t.Fatalf("missing OCI graph blob did not report a plugin error: %s", output)
		}
		if err := (artifacts.Store{Dir: options.CacheDir}).Publish(t.Context(), descriptor.Hex(), cached); err != nil {
			t.Fatal(err)
		}
	}
	pluginCommandTest(t, acquisition, 1, check...)
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(after, config) {
		t.Fatalf("OCI checks modified configuration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("OCI candidate created a project lock: %v", err)
	}
}

// Authors rebuild candidates without consuming release versions. An explicit
// update must select the rebuilt bytes, while ordinary checks keep executing the
// earlier imported bytes until the author asks for that update.
func TestCandidateCLIUpdatesSameVersion(t *testing.T) {
	root, registry, digest, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
	t.Chdir(candidateProject(t))
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
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
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	configPath := filepath.Join(".github", "actionlint.yaml")
	if got := candidateDigest(t, configPath); got != digest {
		t.Fatalf("ordinary check changed candidate digest to %q", got)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "update", "publisher/plugin")
	if got := candidateDigest(t, configPath); got != next {
		t.Fatalf("explicit update digest = %q, want %q", got, next)
	}
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if err := os.Remove(filepath.Join(acquisition.UserCacheDir, "actionlint", "artifacts", next)); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	pluginCommandTest(t, acquisition, 0, "plugin", "sync")
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
}

func TestCandidateProjectsKeepSeparateSameVersionDigests(t *testing.T) {
	root, registry, firstDigest, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
	firstProject, secondProject := t.TempDir(), t.TempDir()
	candidateProjectAt(t, firstProject)
	candidateProjectAt(t, secondProject)
	t.Chdir(firstProject)
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	firstConfig := filepath.Join(firstProject, ".github", "actionlint.yaml")
	if got := candidateDigest(t, firstConfig); got != firstDigest {
		t.Fatalf("first install digest = %q, want %q", got, firstDigest)
	}

	artifactPath := filepath.Join(root, "artifacts", "plugin.wasm")
	original, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := bytes.Replace(original, []byte("Test plugin"), []byte("Next plugin"), 1)
	if bytes.Equal(original, rebuilt) {
		t.Fatal("candidate rebuild did not change bytes")
	}
	secondDigest := fmt.Sprintf("%x", sha256.Sum256(rebuilt))
	if err := os.WriteFile(artifactPath, rebuilt, 0600); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(root, "plugins", "no-sudo", "versions", "v1.json")
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(firstDigest), []byte(secondDigest)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(secondProject)
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	secondConfig := filepath.Join(secondProject, ".github", "actionlint.yaml")
	if got := candidateDigest(t, secondConfig); got != secondDigest {
		t.Fatalf("second install digest = %q, want %q", got, secondDigest)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	for _, project := range []struct {
		name, config, digest string
	}{{"first", firstConfig, firstDigest}, {"second", secondConfig, secondDigest}} {
		t.Run(project.name, func(t *testing.T) {
			t.Chdir(filepath.Dir(filepath.Dir(project.config)))
			output := pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
			if !strings.Contains(output, "[no-sudo]") || candidateDigest(t, project.config) != project.digest {
				t.Fatalf("project lost its selected candidate %s: %s", project.digest, output)
			}
		})
	}
	if len(network.Requests()) != 0 {
		t.Fatalf("local candidates contacted the network: %v", network.Requests())
	}
}

// A broken rebuild must leave an installed candidate usable. Repairing its
// declaration should then allow an explicit update without resetting the project.
func TestCandidateCLIFailedUpdateKeepsInstalledBytes(t *testing.T) {
	root, registry, digest, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
	t.Chdir(candidateProject(t))
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	configPath := filepath.Join(".github", "actionlint.yaml")
	beforeConfig, err := os.ReadFile(configPath)
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
	output := pluginCommandTest(t, acquisition, 3, "plugin", "update", "publisher/plugin")
	if !strings.Contains(output, "SHA256 mismatch") {
		t.Fatalf("wrong candidate failure: %s", output)
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(afterConfig, beforeConfig) || candidateDigest(t, configPath) != digest {
		t.Fatalf("failed update changed candidate declaration: %v", err)
	}
	cache := acquisition.UserCacheDir
	cached, err := os.ReadFile(filepath.Join(cache, "actionlint", "artifacts", digest))
	if err != nil || !bytes.Equal(cached, previous) {
		t.Fatalf("failed update changed installed bytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "actionlint", "artifacts", wrongDigest)); !os.IsNotExist(err) {
		t.Fatalf("failed update cached unverified bytes: %v", err)
	}
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	next := fmt.Sprintf("%x", sha256.Sum256(rebuilt))
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(digest), []byte(next)), 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "update", "publisher/plugin")
	if got := candidateDigest(t, configPath); got != next {
		t.Fatalf("repaired rebuild digest = %q, want %q", got, next)
	}
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if len(network.Requests()) != 0 {
		t.Fatalf("candidate update contacted the registry: %v", network.Requests())
	}
}

// A syntactically relative path can still leave the catalogue through a symlink.
// The target contains the declared bytes with the correct digest. Only the path
// escape should cause rejection, before either project file is written.
func TestCandidateCLIRejectsArtifactSymlinkEscape(t *testing.T) {
	root, registry, _, network := candidateCatalogue(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
	file := filepath.Join(root, "artifacts", "plugin.wasm")
	outside := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.Rename(file, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	t.Chdir(candidateProject(t))
	pluginCommandTest(t, acquisition, 3, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
	if _, err := os.Stat(filepath.Join(".github", "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
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
			root, registry, _, network := candidateCatalogue(t)
			acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
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
			t.Chdir(candidateProject(t))
			pluginCommandTest(t, acquisition, 3, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", registry)
			for _, name := range []string{"actionlint.plugins.lock.json", "actionlint.yaml"} {
				if _, err := os.Stat(filepath.Join(".github", name)); !os.IsNotExist(err) {
					t.Fatalf("failed candidate wrote %s: %v", name, err)
				}
			}
		})
	}
}
