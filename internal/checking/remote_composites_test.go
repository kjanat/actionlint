package checking

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/tempdir"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func remoteCompositeProvider(t *testing.T, manifests map[string]string) (manifestProvider, func() map[string]int) {
	t.Helper()
	return remoteCompositeProviderAtCommits(t, manifests, nil)
}

func remoteCompositeProviderAtCommits(t *testing.T, manifests, commits map[string]string) (manifestProvider, func() map[string]int) {
	t.Helper()
	var mu sync.Mutex
	requests := map[string]int{}
	provider := manifestProvider(func(_ context.Context, ref actions.Reference) (actions.Manifest, error) {
		mu.Lock()
		defer mu.Unlock()
		key := ref.Repository()
		if ref.Directory() != "" {
			key += "/" + ref.Directory()
		}
		requests[key]++
		body, found := manifests[key]
		if !found {
			return actions.UnavailableManifest(availability.NotFetched()), nil
		}
		commit := commits[ref.Repository()]
		if commit == "" {
			commit = strings.Repeat("a", 40)
		}
		identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, commit, ref.Directory())
		if err != nil {
			return actions.Manifest{}, err
		}
		route, err := actions.RefRoute("refs/tags/" + ref.Ref())
		if err != nil {
			return actions.Manifest{}, err
		}
		selection, err := actions.NewSelection(ref, identity, identity.Commit(), route, "test")
		if err != nil {
			return actions.Manifest{}, err
		}
		return actions.NewManifest(selection, []byte(body))
	})
	return provider, func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		return maps.Clone(requests)
	}
}

func TestRemoteCompositeContainerRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, runner, condition string
		want                    int
	}{
		{"windows", "windows-latest", "true", 1},
		{"macos", "macos-latest", "true", 1},
		{"linux", "ubuntu-latest", "true", 0},
		{"skipped", "windows-latest", "false", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inner := "name: inner\ndescription: inner\nruns:\n  using: composite\n  steps:\n    - uses: docker://alpine:3\n"
			provider, requests := remoteCompositeProvider(t, map[string]string{
				"example/outer": "name: outer\ndescription: outer\nruns:\n  using: composite\n  steps:\n    - uses: example/inner@v1\n",
				"example/inner": inner,
			})
			root := t.TempDir()
			workflow := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: %s\n    steps:\n      - uses: example/outer@v1\n        if: %s\n", tc.runner, tc.condition)
			result, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			address := "https://github.com/example/inner/blob/" + strings.Repeat("a", 40) + "/action.yml"
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "if-cond" {
					continue
				}
				count++
				if diagnostic.Rule != "container-runner" || diagnostic.Path != address || diagnostic.Start.Line != 6 {
					t.Fatalf("remote Docker finding has wrong rule or source: %+v", diagnostic)
				}
			}
			if count != tc.want {
				t.Fatalf("runner %s, condition %s: got diagnostics %+v, want %d container findings", tc.runner, tc.condition, result.Diagnostics, tc.want)
			}
			if got := string(result.CapturedSources[address].Content); got != inner {
				t.Fatalf("captured remote manifest = %q, want %q", got, inner)
			}
			for name, count := range requests() {
				if count != 1 {
					t.Errorf("%s requested %d times, want 1", name, count)
				}
			}
		})
	}
}

type remoteGraphChecker struct{ inspect func(checkinputs.Input) error }

func (remoteGraphChecker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "remote-graph", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}}
}

func (c remoteGraphChecker) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	return nil, c.inspect(input)
}

func TestRemoteCompositeRepeatedInvocations(t *testing.T) {
	t.Parallel()
	provider, requests := remoteCompositeProvider(t, map[string]string{
		"example/outer": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: example/inner@v1}]}\n",
		"example/inner": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo nested}]}\n",
	})
	checked := false
	checker := remoteGraphChecker{inspect: func(input checkinputs.Input) error {
		checked = true
		if len(input.Invoked) != 2 || input.CallGraph.CallCount() != 4 {
			return fmt.Errorf("want two scripts in four calls, got scripts=%d graph=%+v", len(input.Invoked), input.CallGraph.Export())
		}
		left, right := input.Invoked[0], input.Invoked[1]
		if left.Reference != right.Reference || left.Occurrence.Owner == right.Occurrence.Owner || left.Occurrence.Ancestry[1] == right.Occurrence.Ancestry[1] {
			return fmt.Errorf("repeated remote source lost distinct calls: %+v, %+v", left.Occurrence, right.Occurrence)
		}
		if !strings.HasPrefix(left.Reference.Document, "https://github.com/example/inner/blob/") {
			return fmt.Errorf("script has wrong source: %+v", left.Reference)
		}
		return nil
	}}
	root := t.TempDir()
	workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/outer@v1}, {uses: example/outer@v1}]}}\n"
	_, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider, Analyzers: []checkinputs.Registration{{Checker: checker}}}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("plugin did not receive the invocation graph")
	}
	for name, count := range requests() {
		if count != 1 {
			t.Errorf("%s requested %d times, want 1", name, count)
		}
	}
}

func TestRemoteCompositeCycleAndUnavailableBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
	}{
		{"cycle", "name: loop\ndescription: loop\nruns: {using: composite, steps: [{uses: example/action@v1}]}\n"},
		{"missing", ""},
		{"invalid", "name: [\n"},
		{"invalid metadata", "name: &name invalid\nruns: {using: composite, steps: [{shell: bash, run: echo invalid}]}\n"},
		{"invalid self reference", "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: '$/nested'}]}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			manifests := map[string]string{}
			if tc.body != "" {
				manifests["example/action"] = tc.body
			}
			if tc.name == "invalid self reference" {
				manifests["example/action/nested"] = "name: &name invalid\nruns: {using: composite, steps: [{shell: bash, run: echo invalid}]}\n"
			}
			provider, requests := remoteCompositeProvider(t, manifests)
			checked := false
			checker := remoteGraphChecker{inspect: func(input checkinputs.Input) error {
				checked = true
				calls := input.CallGraph.Export().Calls
				if len(calls) == 0 {
					return errors.New("missing remote call in graph")
				}
				last := calls[len(calls)-1].Outcome.Value()
				valid := false
				switch tc.name {
				case "cycle":
					_, valid = last.(checkinputs.CycleCall)
				case "missing":
					_, valid = last.(checkinputs.UnavailableCall)
				case "invalid", "invalid metadata", "invalid self reference":
					_, valid = last.(checkinputs.UnresolvedCall)
				}
				if !valid || len(input.Invoked) != 0 {
					return fmt.Errorf("%s produced outcome %#v and %d scripts", tc.name, last, len(input.Invoked))
				}
				return nil
			}}
			root := t.TempDir()
			workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"
			_, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider, Analyzers: []checkinputs.Registration{{Checker: checker}}}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			if !checked || requests()["example/action"] != 1 {
				t.Fatalf("graph checked=%t, requests=%v", checked, requests())
			}
		})
	}
}

func TestRemoteCompositeDepthLimit(t *testing.T) {
	t.Parallel()
	limit := invocation.DefaultBudget().MaxDepth
	manifests := map[string]string{}
	for i := range limit + 3 {
		manifests[fmt.Sprintf("example/action%d", i)] = fmt.Sprintf("name: nested\ndescription: nested\nruns: {using: composite, steps: [{uses: example/action%d@v1}]}\n", i+1)
	}
	provider, requests := remoteCompositeProvider(t, manifests)
	checked := false
	checker := remoteGraphChecker{inspect: func(input checkinputs.Input) error {
		checked = true
		calls := input.CallGraph.Export().Calls
		if len(calls) != limit+1 {
			return fmt.Errorf("depth limit %d produced %d calls", limit, len(calls))
		}
		if _, ok := calls[len(calls)-1].Outcome.Value().(checkinputs.DepthLimitedCall); !ok {
			return fmt.Errorf("last call did not report the depth limit: %+v", calls[len(calls)-1])
		}
		return nil
	}}
	root := t.TempDir()
	workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action0@v1}]}}\n"
	_, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider, Analyzers: []checkinputs.Registration{{Checker: checker}}}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
	if err != nil {
		t.Fatal(err)
	}
	if !checked || len(requests()) != limit {
		t.Fatalf("depth limit did not stop acquisition: checked=%t, requests=%v", checked, requests())
	}
}

func TestRemoteCompositeCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider, _ := remoteCompositeProvider(t, map[string]string{
		"example/outer": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: example/inner@v1}]}\n",
	})
	canceling := manifestProvider(func(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
		if ref.Repository() == "example/inner" {
			cancel()
			return actions.Manifest{}, ctx.Err()
		}
		return provider.Lookup(ctx, ref)
	})
	root := t.TempDir()
	workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/outer@v1}]}}\n"
	_, err := Analyze(ctx, Options{WorkingDir: root, RemoteActions: canceling}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("nested lookup cancellation returned %v", err)
	}
}

type compositeMirrorProvider struct {
	provider actions.Provider
	requests *mirrorRequests
	access   references.MirrorAccess
}

func (p compositeMirrorProvider) WithMetadataMirrors(access references.MirrorAccess, _ []string) actions.Provider {
	p.access = access
	return p
}

func (p compositeMirrorProvider) Lookup(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	p.requests.Lock()
	p.requests.actions[p.access]++
	p.requests.Unlock()
	return p.provider.Lookup(ctx, ref)
}

func TestRemoteCompositeUsesCallerMirrorConfiguration(t *testing.T) {
	t.Parallel()
	provider, _ := remoteCompositeProvider(t, map[string]string{
		"example/outer": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: example/inner@v1}]}\n",
		"example/inner": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{uses: docker://alpine:3}]}\n",
	})
	requests := &mirrorRequests{actions: map[references.MirrorAccess]int{}}
	request := analysisRequest{WorkingDir: t.TempDir(), RemoteActions: compositeMirrorProvider{provider: provider, requests: requests}}
	for i, setting := range []string{`{}`, `{"allow-unknown-repositories":true}`, `{"services":[]}`} {
		cfg, err := ConfigurationRules().Parse([]byte(`{"network":{"metadata-mirrors":` + setting + `}}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Sources = append(request.Sources, sourceUnit{
			Path: fmt.Sprintf("workflow-%d.yml", i), Config: cfg,
			Content: []byte("on: push\npermissions: {}\njobs: {build: {runs-on: windows-latest, steps: [{uses: example/outer@v1}]}}\n"),
		})
	}
	result, err := runTestAnalysis(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want one deduplicated container-runner finding, got %+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule != "container-runner" {
			t.Errorf("unexpected finding: %+v", diagnostic)
		}
	}
	for _, access := range []references.MirrorAccess{references.PublicMirrorsOnly, references.AllowUnknownMirrors, references.NoMirrors} {
		if requests.actions[access] != 2 {
			t.Errorf("mirror setting %v: got %d requests, want one for each nested action", access, requests.actions[access])
		}
	}
}

func TestRemoteCompositeRelativeActions(t *testing.T) {
	t.Parallel()
	// GitHub defines $/ relative to the action repository and ./ relative to the workspace:
	// https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax#runsstepsuses
	for _, tc := range []struct{ uses, expected string }{
		{"$/nested", "remote"},
		{"./nested", "local"},
	} {
		t.Run(tc.expected, func(t *testing.T) {
			t.Parallel()
			provider, requests := remoteCompositeProvider(t, map[string]string{
				"example/action":        "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: '" + tc.uses + "'}]}\n",
				"example/action/nested": "name: remote\ndescription: remote\nruns: {using: composite, steps: [{shell: bash, run: echo remote}]}\n",
			})
			root := t.TempDir()
			directory := filepath.Join(root, "nested")
			if err := os.Mkdir(directory, 0o755); err != nil {
				t.Fatal(err)
			}
			local := "name: local\ndescription: local\nruns: {using: composite, steps: [{shell: bash, run: echo local}]}\n"
			if err := os.WriteFile(filepath.Join(directory, "action.yml"), []byte(local), 0o644); err != nil {
				t.Fatal(err)
			}
			checked := false
			checker := remoteGraphChecker{inspect: func(input checkinputs.Input) error {
				checked = true
				if len(input.Invoked) != 1 {
					return fmt.Errorf("want one script, got %+v", input.CallGraph.Export())
				}
				document := input.Invoked[0].Reference.Document
				remote := strings.HasPrefix(document, "https://github.com/example/action/blob/")
				if remote != (tc.expected == "remote") {
					return fmt.Errorf("%s resolved to wrong repository: %s", tc.uses, document)
				}
				return nil
			}}
			workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n"
			_, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider, Analyzers: []checkinputs.Registration{{Checker: checker}}}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			if !checked {
				t.Fatal("invocations were not checked")
			}
			if tc.expected == "remote" && requests()["example/action/nested"] != 1 {
				t.Fatalf("self reference was not acquired once: %v", requests())
			}
		})
	}
}

func TestRemoteCompositeRepositorySwitches(t *testing.T) {
	t.Parallel()
	for _, middle := range []string{"./middle", "example/second@v2"} {
		t.Run(middle, func(t *testing.T) {
			t.Parallel()
			root := tempdir.Physical(t)
			leaf := "name: leaf\ndescription: leaf\nruns: {using: composite, steps: [{shell: bash, run: echo leaf}]}\n"
			intermediate := "name: middle\ndescription: middle\nruns: {using: composite, steps: [{uses: '$/nested'}]}\n"
			for name, body := range map[string]string{"middle": intermediate, "nested": leaf} {
				directory := filepath.Join(root, name)
				if err := os.Mkdir(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "action.yml"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			secondCommit := strings.Repeat("b", 40)
			provider, requests := remoteCompositeProviderAtCommits(t, map[string]string{
				"example/outer":  "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: '" + middle + "'}]}\n",
				"example/second": intermediate, "example/second/nested": leaf,
			}, map[string]string{"example/second": secondCommit})
			pinned := manifestProvider(func(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
				if ref.Directory() != "" && (ref.Repository() != "example/second" || ref.Ref() != secondCommit) {
					t.Errorf("nested action requested from %s/%s@%s, want example/second/nested@%s", ref.Repository(), ref.Directory(), ref.Ref(), secondCommit)
				}
				return provider.Lookup(ctx, ref)
			})
			checked := false
			checker := remoteGraphChecker{inspect: func(input checkinputs.Input) error {
				checked = true
				if len(input.Invoked) != 1 || input.CallGraph.CallCount() != 3 {
					return fmt.Errorf("want one script across three calls, got scripts=%d graph=%+v", len(input.Invoked), input.CallGraph.Export())
				}
				want := filepath.ToSlash(filepath.Join(root, "nested", "action.yml"))
				if middle == "example/second@v2" {
					want = "https://github.com/example/second/blob/" + secondCommit + "/nested/action.yml"
				}
				if got := filepath.ToSlash(input.Invoked[0].Reference.Document); got != want {
					return fmt.Errorf("script source = %s, want %s", got, want)
				}
				return nil
			}}
			workflow := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/outer@v1}]}}\n"
			_, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: pinned, Analyzers: []checkinputs.Registration{{Checker: checker}}}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(workflow)}})
			if err != nil {
				t.Fatal(err)
			}
			wantRequests := map[string]int{"example/outer": 1}
			if middle == "example/second@v2" {
				wantRequests["example/second"], wantRequests["example/second/nested"] = 1, 1
			}
			if !checked || !maps.Equal(requests(), wantRequests) {
				t.Fatalf("checked=%t, manifest requests=%v, want %v", checked, requests(), wantRequests)
			}
		})
	}
}

func TestRemoteCompositeAdviceRemainsVisible(t *testing.T) {
	t.Parallel()
	for _, failOnAll := range []bool{false, true} {
		t.Run(strconv.FormatBool(failOnAll), func(t *testing.T) {
			t.Parallel()
			provider, _ := remoteCompositeProvider(t, map[string]string{
				"example/action": "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo \"${{ github.workspace }}\"\n",
			})
			cfg, err := ConfigurationRules().Parse([]byte(fmt.Sprintf("lint: {dependencies: {fail-on-all: %t}}", failOnAll)))
			if err != nil {
				t.Fatal(err)
			}
			result, err := runTestAnalysis(t.Context(), analysisRequest{
				WorkingDir: t.TempDir(), RemoteActions: provider,
				Sources: []sourceUnit{{Path: "ci.yml", Config: cfg, Content: []byte("on: push\npermissions: {}\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one interpolation finding, got %+v", result.Diagnostics)
			}
			d := result.Diagnostics[0]
			if d.Rule != "script-interpolation" || d.NonBlocking == failOnAll || !strings.HasPrefix(d.Path, "https://github.com/example/action/") {
				t.Fatalf("fail-on-all=%v: unexpected dependency finding %+v", failOnAll, d)
			}
		})
	}
}

func TestLocalActionDependencyFailureSettings(t *testing.T) {
	t.Parallel()
	for _, override := range []bool{false, true} {
		t.Run(strconv.FormatBool(override), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			provider, _ := remoteCompositeProvider(t, map[string]string{
				"example/action": "name: example\ndescription: example\nruns: {using: composite, steps: [{shell: bash, run: 'echo ${{ github.workspace }}'}]}\n",
			})
			config := "lint: {dependencies: {fail-on-all: true}}\n"
			if override {
				config += "overrides: [{includes: ['action.yml'], lint: {dependencies: {fail-on-all: false}}}]\n"
			}
			cfg, err := ConfigurationRules().Parse([]byte(config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider}, []workspace.Source{{
				Path: "action.yml", Project: workspace.Identity(root), Config: cfg,
				Content: []byte("name: local\ndescription: local\nruns: {using: composite, steps: [{uses: example/action@v1}]}\n"),
			}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-interpolation" || result.Diagnostics[0].NonBlocking != override {
				t.Fatalf("override=%v: unexpected dependency findings %+v", override, result.Diagnostics)
			}
		})
	}
}
