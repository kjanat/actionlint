package githubaction

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginPlanNestedRepositories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, files        string
		defaults, absolute bool
	}{
		{"exact declarations with directories", ".github/workflows\nchild/.github/workflows", false, false},
		{"exact declarations with absolute directories", ".github/workflows\nchild/.github/workflows", false, true},
		{"exact declarations with globs", ".github/workflows/*.yml\nchild/.github/workflows/*.yml", false, false},
		{"defaults with directories", ".github/workflows\nchild/.github/workflows", true, false},
		{"defaults with absolute globs", ".github/workflows/*.yml\nchild/.github/workflows/*.yml", true, true},
		{"defaults with globs", ".github/workflows/*.yml\nchild/.github/workflows/*.yml", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wasmA := []byte{0, 97, 115, 109, 13, 0, 1, 0}
			wasmB := append(bytes.Clone(wasmA), 0, 2, 1, 'b')
			wasmC := append(bytes.Clone(wasmA), 0, 2, 1, 'c')
			var registry *fixtureregistry.PluginRegistry
			var rootConfig, childConfig string
			var want []string
			disable := "shellcheck,ruff"
			if tc.defaults {
				registry = fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
				index := registry.File(plugininstall.DefaultURL + "index.json")
				ruff := registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "ruff", BaseURL: plugininstall.DefaultURL, Name: "github.com/kjanat/actionlint-plugin-ruff", Aliases: []string{"ruff"}, RepositoryID: 11}, "v1.0.0", wasmB)
				registry.SetFile(plugininstall.DefaultURL+"index.json", index)
				rootConfig = "tools: {ruff: false}\n"
				childConfig = "tools: {shellcheck: false}\n"
				disable = ""
				want = []string{digestForPlan(registry.File("https://github.com/kjanat/actionlint-plugin-shellcheck/releases/download/v1.0.0/plugin.wasm")), digestForPlan(ruff)}
			} else {
				registry = fixtureregistry.NewPluginRegistry(t)
				var refs []string
				for i, data := range [][]byte{wasmA, wasmB, wasmC} {
					name := []string{"one", "shared", "two"}[i]
					base := "https://" + name + ".example/"
					artifact := registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: name, BaseURL: base, Name: "github.com/publisher/" + name, RepositoryID: 100 + i}, "v1.0.0", data)
					want = append(want, digestForPlan(artifact))
					refs = append(refs, "  - {name: github.com/publisher/"+name+", version: v1.0.0, registry: "+base+"}\n")
				}
				rootConfig = "plugins:\n" + refs[0] + refs[1]
				childConfig = "plugins:\n" + refs[1] + refs[2]
			}
			root := workspaceWith(t, map[string]string{
				".git": "", ".github/workflows/one.yml": cleanWorkflow, "child/.github/workflows/two.yml": cleanWorkflow,
				".github/actionlint.yml": rootConfig, "child/.github/actionlint.yml": childConfig,
			})
			child := filepath.Join(root, "child")
			command := exec.CommandContext(t.Context(), "git", "init", "--quiet", child)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("init nested repository: %v: %s", err, output)
			}
			inputFiles := tc.files
			if tc.absolute {
				paths := strings.Split(inputFiles, "\n")
				for i, path := range paths {
					paths[i] = filepath.Join(root, filepath.FromSlash(path))
				}
				inputFiles = strings.Join(paths, "\n")
			}
			env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_FILES": inputFiles, "INPUT_DISABLE-TOOLS": disable}
			acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			var stdout, stderr bytes.Buffer
			if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr, acquisition); code != 0 {
				t.Fatalf("PluginPlan returned %d: %s", code, &stderr)
			}
			var plan plugininstall.CachePlan
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			slices.Sort(want)
			if plan.Directory != pluginCacheDirectory(registry.UserCacheDir()) || !reflect.DeepEqual(plan.Artifacts, want) {
				t.Fatalf("plan = %+v, want directory %q and artifacts %v", plan, pluginCacheDirectory(registry.UserCacheDir()), want)
			}
			for _, request := range registry.Requests() {
				if strings.HasSuffix(request, "/plugin.wasm") {
					t.Fatalf("plan downloaded plugin artifact: %v", registry.Requests())
				}
			}
		})
	}
}

func digestForPlan(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
