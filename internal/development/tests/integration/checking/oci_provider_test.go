package checking_test

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

	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/ocitest"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
	"actionlint.kjanat.dev/v2/internal/results"
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

func ociFindings(t *testing.T, result *checking.Result) []results.Diagnostic {
	t.Helper()
	return slices.DeleteFunc(slices.Clone(result.Diagnostics), func(d results.Diagnostic) bool { return d.Rule != "oci-tool" })
}

// A provider pulled from an OCI registry runs its stored executable through
// the process transport, and keeps running offline from the store.
func TestOCIBinaryProviderRunsFromStore(t *testing.T) {
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
	pkg := publishOCIProvider(t)
	check := func() *checking.Result {
		t.Helper()
		result, err := checking.CheckWithPluginPaths(t.Context(), checking.Options{}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(ociProviderInput)}, []string{pkg.reference}, acquisition)
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
	materialized := filepath.Join(acquisition.ArtifactOptions().CacheDir, "exec", pkg.executable.Hex(), name)
	if _, err := os.Stat(materialized); err != nil {
		t.Fatalf("executable not materialized: %v", err)
	}
	pkg.registry.Close()
	if findings := ociFindings(t, check()); len(findings) != 1 {
		t.Fatalf("offline findings = %+v", findings)
	}
}

// A configuration file cannot authorize its private OCI endpoint, even when
// the invocation authorizes native execution. Selecting --config grants no
// network authority either.
func TestOCIBinaryProviderConfigurationCannotGrantPrivateNetwork(t *testing.T) {
	t.Parallel()
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "config-flag"}[explicit], func(t *testing.T) {
			t.Parallel()
			acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
			pkg := publishOCIProvider(t)
			root := repositoryfixture.Directory(t)
			path := filepath.Join(root, ".github", "actionlint.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			fixturefiles.WriteText(t, path, "plugins: ['"+pkg.reference+"']\n")
			selection := plugininstall.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}
			if explicit {
				selection.ConfigFile = path
			}
			input := func() inputmodel.Request {
				return inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(ociProviderInput)}
			}
			sources, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sources.Paths, []string{pkg.reference}) {
				t.Fatalf("selected %q", sources.Paths)
			}
			if _, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, input(), sources); err == nil || !strings.Contains(err.Error(), "public destination") {
				t.Fatalf("repository-selected private endpoint was contacted: %v", err)
			}
			if got := pkg.registry.Fetches(pkg.executable); got != 0 {
				t.Fatalf("executable downloaded %d times without opt-in", got)
			}
			if _, err := os.Stat(filepath.Join(acquisition.ArtifactOptions().CacheDir, "exec")); !os.IsNotExist(err) {
				t.Fatalf("executable materialized without opt-in: %v", err)
			}
			if _, err := checking.CheckWithPlugins(t.Context(), checking.Options{AllowConfigProcesses: true}, input(), sources); err == nil || !strings.Contains(err.Error(), "public destination") {
				t.Fatalf("process permission granted private network access: %v", err)
			}
		})
	}
}

func TestOCIBinaryProviderDisabledByID(t *testing.T) {
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
	pkg := publishOCIProvider(t)
	sources, err := plugininstall.SourcesFromArguments([]string{pkg.reference}, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	sources.DisabledTools, sources.NamedTools = []string{"oci-tool"}, []string{"oci-tool"}
	result, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(ociProviderInput)}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if findings := ociFindings(t, result); len(findings) != 0 {
		t.Fatalf("disabled provider ran: %+v", findings)
	}
	if _, err := os.Stat(filepath.Join(acquisition.ArtifactOptions().CacheDir, "exec")); !os.IsNotExist(err) {
		t.Fatalf("disabled provider's executable was materialized: %v", err)
	}
	if got := pkg.registry.Fetches(pkg.executable); got != 0 {
		t.Fatalf("disabled provider's executable was downloaded %d times", got)
	}
}

// OCI manifests supply the complete host cache plan without a project lock.
func TestCachePlanListsOCIBlobs(t *testing.T) {
	t.Parallel()
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir()}
	pkg := publishOCIProvider(t)
	_, root, _ := strings.Cut(pkg.reference, "@sha256:")
	sources, err := plugininstall.SourcesFromArguments([]string{pkg.reference}, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := sources.CachePlan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Artifacts, root) || !slices.Contains(plan.Artifacts, pkg.executable.Hex()) || len(plan.Artifacts) != 5 {
		t.Fatalf("incomplete manifest cache plan: %q", plan.Artifacts)
	}
	if got := pkg.registry.Fetches(pkg.executable); got != 0 {
		t.Fatalf("planning fetched executable %d times", got)
	}
	ref, err := pluginref.Parse(pkg.reference)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := pluginartifact.ExplicitSource(pkg.reference)
	if err != nil {
		t.Fatal(err)
	}
	project := plugininstall.Project{Manifest: &configplugins.File{References: []pluginref.Reference{ref}}, StatusOptions: plugininstall.StatusOptions{Network: grant, Acquisition: acquisition}}
	if err := project.SyncDirect(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, digest := range plan.Artifacts {
		if _, err := os.Stat(filepath.Join(plan.Directory, digest)); err != nil {
			t.Fatalf("sync did not store %s: %v", digest, err)
		}
	}
}
