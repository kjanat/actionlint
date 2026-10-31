package localapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/testutil/ocitest"
)

// ociProvider is a binary tool provider published to an OCI registry: an
// index whose image manifest for this host holds the provider manifest, the
// jq transform it names and the executable, which is this test binary.
type ociProvider struct {
	registry   *ocitest.Registry
	reference  string
	executable ocitest.Descriptor
}

func publishOCIProvider(t *testing.T) ociProvider {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	const program = `[.items[] | {filename: $vars.filename, code: "OCI1", message: .text, location: {row: .line, column: .col}, end_location: {row: .line, column: (.col + 4)}}]`
	transform := registry.Blob("application/vnd.actionlint.jq", []byte(program))
	executable := registry.Blob("application/octet-stream", binary)
	sum := sha256.Sum256(binary)
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": "example.com/oci-tool", "version": "1.0.0", "role": "script", "scope": "source", "transport": "process",
		"rule":         map[string]any{"name": "oci-tool", "description": "A tool from an OCI registry", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "oci-tool"},
		"artifacts":    []map[string]any{{"platform": pluginartifact.HostPlatform(), "digest": "sha256:" + hex.EncodeToString(sum[:]), "filename": "oci-tool", "size": len(binary)}},
		"preparation":  "kjanat:actionlint/expression-mask@0.1.0",
		"selectors":    map[string]any{"shells": []string{"bash"}, "filename": "actionlint.sh"},
		"capabilities": map[string]any{"fixes": false, "reported_sources": false, "message_template": "oci: {code}: {message}"},
		"process": map[string]any{
			// The entry names the command; the store supplies the executable.
			"entry": "oci-tool", "args": []string{"-test.run=^TestLocalManifestToolHelper$", "--", "{var:filename}"}, "stdin": "source",
			"env": map[string]string{"ACTIONLINT_LOCAL_MANIFEST_HELPER": "1"}, "env_passthrough": []string{"TMPDIR", "TEMP", "TMP"},
			"config": map[string]any{"render": "none"},
			"output": map[string]any{"format": "json", "transform": map[string]any{"language": "jq", "digest": transform.Digest}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	layers := []ocitest.Descriptor{registry.Blob(pluginartifact.MediaTypeProviderManifest, manifest), transform, executable}
	index := registry.Index(t, map[string]ocitest.Descriptor{pluginartifact.HostPlatform(): registry.Manifest(t, layers...)})
	return ociProvider{registry: registry, reference: registry.Reference(index), executable: executable}
}

const ociProviderInput = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ github.ref }}\n        shell: bash\n"

func ociFindings(t *testing.T, result *application.Result) []diagnostic.Diagnostic {
	t.Helper()
	return slices.DeleteFunc(slices.Clone(result.Diagnostics), func(d diagnostic.Diagnostic) bool { return d.Rule != "oci-tool" })
}

// A provider pulled from an OCI registry runs its stored executable through
// the process transport, and keeps running offline from the store.
func TestOCIBinaryProviderRunsFromStore(t *testing.T) {
	testutil.IsolateUserCache(t)
	pkg := publishOCIProvider(t)
	check := func() *application.Result {
		t.Helper()
		result, err := Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(ociProviderInput)}, []string{pkg.reference})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	findings := ociFindings(t, check())
	want := "oci: OCI1: saw echo " + strings.Repeat("_", len("${{ github.ref }}"))
	if len(findings) != 1 || findings[0].Message != want || findings[0].Start.Line != 6 {
		t.Fatalf("findings = %+v, want message %q", findings, want)
	}
	name := "oci-tool"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	materialized := filepath.Join(artifactOptions().CacheDir, "exec", pkg.executable.Hex(), name)
	if _, err := os.Stat(materialized); err != nil {
		t.Fatalf("executable not materialized: %v", err)
	}
	pkg.registry.Close()
	if findings := ociFindings(t, check()); len(findings) != 1 {
		t.Fatalf("offline findings = %+v", findings)
	}
}

// A binary provider declared in repository configuration, found
// automatically or through --config, runs only with
// --allow-config-processes. Without it the executable is never downloaded
// or materialized.
func TestOCIBinaryProviderFromConfigurationNeedsOptIn(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "config-flag"}[explicit], func(t *testing.T) {
			testutil.IsolateUserCache(t)
			pkg := publishOCIProvider(t)
			root := pluginRepository(t)
			path := filepath.Join(root, ".github", "actionlint.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			writeManifestTest(t, path, "plugins: ['"+pkg.reference+"']\n")
			selection := PluginSelection{}
			if explicit {
				selection.ConfigFile = path
			}
			input := func() application.Input {
				return application.Input{Kind: application.Stdin, Reader: strings.NewReader(ociProviderInput)}
			}
			sources, err := selection.Resolve(t.Context(), root, application.Input{Kind: application.Repository})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sources.Paths, []string{pkg.reference}) {
				t.Fatalf("selected %q", sources.Paths)
			}
			if _, err := CheckSources(t.Context(), application.Options{}, input(), sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
				t.Fatalf("repository-selected binary provider ran without opt-in: %v", err)
			}
			if got := pkg.registry.Fetches(pkg.executable); got != 0 {
				t.Fatalf("executable downloaded %d times without opt-in", got)
			}
			if _, err := os.Stat(filepath.Join(artifactOptions().CacheDir, "exec")); !os.IsNotExist(err) {
				t.Fatalf("executable materialized without opt-in: %v", err)
			}
			result, err := CheckSources(t.Context(), application.Options{AllowConfigProcesses: true}, input(), sources)
			if err != nil {
				t.Fatal(err)
			}
			if len(ociFindings(t, result)) != 1 {
				t.Fatalf("findings = %+v", result.Diagnostics)
			}
		})
	}
}

func TestOCIBinaryProviderDisabledByID(t *testing.T) {
	testutil.IsolateUserCache(t)
	pkg := publishOCIProvider(t)
	sources := PluginSources{Paths: []string{pkg.reference}, disabledTools: []string{"oci-tool"}, tools: []string{"oci-tool"}}
	result, err := CheckSources(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(ociProviderInput)}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if findings := ociFindings(t, result); len(findings) != 0 {
		t.Fatalf("disabled provider ran: %+v", findings)
	}
	if _, err := os.Stat(filepath.Join(artifactOptions().CacheDir, "exec")); !os.IsNotExist(err) {
		t.Fatalf("disabled provider's executable was materialized: %v", err)
	}
	if got := pkg.registry.Fetches(pkg.executable); got != 0 {
		t.Fatalf("disabled provider's executable was downloaded %d times", got)
	}
}

// The lock lists the blobs of an oci:// source, so the cache plan names the
// executable before anything is pulled.
func TestCachePlanListsOCIBlobs(t *testing.T) {
	testutil.IsolateUserCache(t)
	pkg := publishOCIProvider(t)
	_, root, _ := strings.Cut(pkg.reference, "@sha256:")
	unlocked := PluginSources{Paths: []string{pkg.reference}}
	plan, err := unlocked.CachePlan()
	if err != nil || !slices.Equal(plan.Artifacts, []string{root}) {
		t.Fatalf("unlocked plan = %+v, %v", plan, err)
	}
	pin, err := describeSource(t.Context(), pkg.reference)
	if err != nil {
		t.Fatal(err)
	}
	locked := PluginSources{Paths: []string{pkg.reference}, lock: &pluginregistry.Lock{SchemaVersion: pluginregistry.LockSchemaVersion, Plugins: []pluginregistry.Pin{}, Sources: []pluginregistry.SourcePin{pin}}}
	plan, err = locked.CachePlan()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Artifacts, root) || !slices.Contains(plan.Artifacts, pkg.executable.Hex()) || len(plan.Artifacts) != 5 {
		t.Fatalf("locked plan = %q", plan.Artifacts)
	}
	// Hydrate runs before every check, so it stores what selecting the
	// provider reads and leaves the executable to an opted-in run.
	if err := locked.Hydrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := pkg.registry.Fetches(pkg.executable); got != 0 {
		t.Fatalf("hydrate downloaded the executable %d times", got)
	}
	// plugin sync stores everything, so a check can run offline.
	if err := SyncPlugins(t.Context(), *locked.lock); err != nil {
		t.Fatal(err)
	}
	for _, digest := range plan.Artifacts {
		if _, err := os.Stat(filepath.Join(plan.Directory, digest)); err != nil {
			t.Fatalf("sync did not store %s: %v", digest, err)
		}
	}
}
