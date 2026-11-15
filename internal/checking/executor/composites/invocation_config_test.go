package composites

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/binding"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

type configVisitProbe struct {
	workflowvisit.Base
	steps    []string
	finished bool
}

func (probe *configVisitProbe) VisitStep(step workflowdecl.Step) error {
	probe.steps = append(probe.steps, step.ID.Text())
	return nil
}

func (probe *configVisitProbe) VisitWorkflowPost(workflowdecl.Workflow) error {
	probe.finished = true
	return nil
}

func TestCompositeConfigFailureIsVisibleWithoutScripts(t *testing.T) {
	t.Parallel()
	for _, collect := range []bool{false, true} {
		t.Run(fmt.Sprintf("scripts=%t", collect), func(t *testing.T) {
			t.Parallel()
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - {id: before, run: echo before}\n      - {id: broken, uses: $/outer}\n      - {id: after, run: echo after}\n  later:\n    runs-on: ubuntu-latest\n    steps: [{id: later-job, run: echo later}]\n"
			f := newInvocationFixture(t, t.Context(), workflow, map[string]string{"outer/action.yml": "name: outer\ndescription: test\nruns: {using: composite, steps: []}\n"}, nil)
			probe := &configVisitProbe{Base: workflowvisit.NewBase("config-visits", "")}
			f.options.Rules = append(f.options.Rules, probe)
			failure := errors.New("invalid action configuration")
			loads := 0
			f.options.CollectScripts = collect
			f.options.BindSource = testSourceBindings(func(string) (*repoconfig.Config, error) {
				loads++
				return nil, failure
			})
			result, err := Workflow(f.options, f.root)
			want := fmt.Sprintf("load configuration for action %q: %s", filepath.Join(f.options.Paths.Workspace, "outer", "action.yml"), failure)
			if !errors.Is(err, failure) || err.Error() != want || loads != 1 || !result.Batch.Invocations().Incomplete() || len(result.Sources) != 1 {
				t.Fatalf("configuration failure lost or hidden: error=%v loads=%d result=%+v", err, loads, result)
			}
			calls := result.Batch.Invocations().Calls()
			if len(calls) != 1 || calls[0].Outcome.Detail() != want {
				t.Fatalf("interruption lost the action path: %+v", calls)
			}
			if !slices.Equal(probe.steps, []string{"before", "broken"}) || probe.finished {
				t.Fatalf("entry continued after config failure: steps=%v workflow-post=%v", probe.steps, probe.finished)
			}
			wantScripts := 0
			if collect {
				wantScripts = 1
			}
			if len(result.Batch.Scripts()) != wantScripts || (collect && result.Batch.Scripts()[0].Run.Run.Text() != "echo before") {
				t.Fatal("interruption lost preceding facts or collected scripts after the failing call")
			}
		})
	}
}

func TestRepeatedCompositeConfigIsResolvedOnceWithoutScripts(t *testing.T) {
	t.Parallel()
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}, {uses: $/outer}]}}\n", map[string]string{"outer/action.yml": "name: outer\ndescription: test\nruns: {using: composite, steps: []}\n"}, nil)
	childConfig := &repoconfig.Config{}
	loads := 0
	f.options.CollectScripts = false
	f.options.BindSource = testSourceBindings(func(path string) (*repoconfig.Config, error) {
		loads++
		return childConfig, nil
	})
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 || len(result.Batch.Scopes()) != 3 {
		t.Fatalf("repeated no-script invocation reloaded/lost config: loads=%d scopes=%d", loads, len(result.Batch.Scopes()))
	}
}

func TestActionInputCatalogueMergesStaticAndPerConfigSites(t *testing.T) {
	t.Parallel()
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/without-inputs@v1\n      - uses: owner/root@v1\n        with: {script: 'root()'}\n      - uses: $/outer\n      - uses: owner/shared@v1\n        with: {script: 'shared()'}\n"
	action := "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: owner/child@v1\n      with: {script: 'child()'}\n    - uses: owner/shared@v1\n      with: {script: 'shared()'}\n"
	f := newInvocationFixture(t, t.Context(), workflow, map[string]string{"outer/action.yml": action}, nil)
	rootConfig, childConfig := &repoconfig.Config{}, &repoconfig.Config{}
	f.options.Bindings = testBindings(rootConfig)
	f.options.BindSource = testSourceBindings(func(string) (*repoconfig.Config, error) { return childConfig, nil })
	f.options.ScriptInputs = []checkinputs.ScriptInput{{Action: "owner/shared", Input: "script", Language: "javascript"}}
	loads := map[*repoconfig.Config]int{}
	f.options.ScriptInputsForScope = func(scope *binding.Scope) ([]checkinputs.ScriptInput, error) {
		config := scope.Composite()
		loads[config]++
		name := "owner/root"
		if config == childConfig {
			name = "owner/child"
		}
		return []checkinputs.ScriptInput{{Action: name, Input: "script", Language: "javascript"}}, nil
	}
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"owner/root", "owner/child", "owner/shared", "owner/shared"}
	if len(result.Batch.Scripts()) != len(want) || loads[rootConfig] != 1 || loads[childConfig] != 1 {
		t.Fatalf("catalogue lost sites or rebuilt per step: scripts=%d loads=%v", len(result.Batch.Scripts()), loads)
	}
	for index, name := range want {
		if input := result.Batch.Scripts()[index].Input; input == nil || input.Site.Action != name {
			t.Fatalf("script %d uses the wrong catalogue: %+v", index, input)
		}
	}
}

func TestActionInputCatalogueUsesDeclaringConfigAndRestoresEntry(t *testing.T) {
	t.Parallel()
	for _, collect := range []bool{false, true} {
		t.Run(fmt.Sprintf("scripts=%t", collect), func(t *testing.T) {
			t.Parallel()
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@v7\n        with: {script: 'console.log(1)'}\n      - uses: $/outer\n      - uses: actions/github-script@v7\n        with: {script: 'console.log(3)'}\n"
			f := newInvocationFixture(t, t.Context(), workflow, map[string]string{"outer/action.yml": "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v7\n      with: {script: 'console.log(2)'}\n"}, nil)
			rootConfig, childConfig := &repoconfig.Config{}, &repoconfig.Config{}
			f.options.Bindings = testBindings(rootConfig)
			f.options.CollectScripts = collect
			f.options.BindSource = testSourceBindings(func(string) (*repoconfig.Config, error) { return childConfig, nil })
			var seen []*repoconfig.Config
			f.options.ScriptInputsForScope = func(scope *binding.Scope) ([]checkinputs.ScriptInput, error) {
				config := scope.Composite()
				seen = append(seen, config)
				return nil, nil
			}
			result, err := Workflow(f.options, f.root)
			if err != nil {
				t.Fatal(err)
			}
			want := []*repoconfig.Config{rootConfig, childConfig, rootConfig}
			if !slices.Equal(seen, []*repoconfig.Config{rootConfig, childConfig}) || len(result.Batch.Invocations().Scripts()) != len(want) {
				t.Fatal("semantic registration must use each declaring config even without script analysis")
			}
			if !collect {
				want = nil
			}
			if len(result.Batch.Scripts()) != len(want) {
				t.Fatalf("collected %d script units, want %d", len(result.Batch.Scripts()), len(want))
			}
			for index, config := range want {
				unit := result.Batch.Scripts()[index]
				if unit.Scope.Config != config {
					t.Fatalf("script %d uses a foreign configuration", index)
				}
			}
		})
	}
}

type ignoreProbeRule struct{ workflowvisit.Base }

func (*ignoreProbeRule) EnterInvocation(checkcalls.Invocation) error { return nil }

func (*ignoreProbeRule) VisitInvocationStep(workflowdecl.Step) error { return nil }

func (*ignoreProbeRule) LeaveInvocation(invocation.Context) error { return nil }

func (rule *ignoreProbeRule) VisitOpaqueAction(step workflowdecl.Step) error {
	if _, action := step.Action(); action {
		pos, _ := step.Position()
		rule.Error(&pos, "probe")
	}
	return nil
}

func TestNonExpandedCallsUseTheirDeclaringConfig(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"opaque", "cycle", "depth"} {
		for _, collect := range []bool{false, true} {
			for _, ignores := range []struct {
				name        string
				root, child bool
			}{{"none", false, false}, {"root", true, false}, {"child", false, true}, {"both", true, true}} {
				name := outcome + "/no-scripts/" + ignores.name
				if collect {
					name = outcome + "/scripts/" + ignores.name
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					target := "owner/action@v1"
					var budget *invocation.Budget
					if outcome == "cycle" {
						target = "$/outer"
					}
					if outcome == "depth" {
						target = "$/nested"
						budget = &invocation.Budget{MaxDepth: 1, MaxCalls: 10, MaxScripts: invocation.DefaultMaxScripts}
					}
					f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{id: outer, uses: $/outer}]}}\n", map[string]string{"outer/action.yml": "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - id: nested\n      uses: " + target + "\n"}, budget)
					rootConfig, childConfig := &repoconfig.Config{}, &repoconfig.Config{}
					if ignores.root {
						rootConfig.Ignores = repoconfig.IgnoreConfigs{{Rule: "probe", Step: "outer"}}
					}
					if ignores.child {
						childConfig.Ignores = repoconfig.IgnoreConfigs{{Rule: "probe", Step: "nested"}}
					}
					f.options.Bindings = testBindings(rootConfig)
					f.options.BindSource = testSourceBindings(func(string) (*repoconfig.Config, error) { return childConfig, nil })
					f.options.CollectScripts = collect
					f.options.Rules = append(f.options.Rules, &ignoreProbeRule{Base: workflowvisit.NewBase("probe", "")})
					result, err := Workflow(f.options, f.root)
					if err != nil {
						t.Fatal(err)
					}
					found := 0
					for _, finding := range result.Findings {
						if finding.Rule != "probe" {
							continue
						}
						found++
						if (finding.IgnoreOrigin != nil) != ignores.child {
							t.Fatalf("nested %s used entry ignore config: %+v", outcome, finding)
						}
						if origin := finding.IgnoreOrigin; origin != nil && (origin.StepID != "nested" || origin.Uses != target) {
							t.Fatalf("nested %s kept its caller's selectors: %+v", outcome, origin)
						}
					}
					if found != 1 {
						t.Fatalf("probe findings=%d, want 1", found)
					}
					if len(result.Batch.Scopes()) != 2 || result.Batch.Scopes()[1].Config != childConfig {
						t.Fatal("no-script invocation lost its file configuration")
					}
				})
			}
		}
	}
}

func TestRootOpaqueCallUsesEntryConfig(t *testing.T) {
	t.Parallel()
	for _, collect := range []bool{false, true} {
		for _, ignores := range []bool{false, true} {
			t.Run(fmt.Sprintf("scripts=%t/ignores=%t", collect, ignores), func(t *testing.T) {
				t.Parallel()
				f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{id: remote, uses: owner/action@v1}]}}\n", nil, nil)
				config := &repoconfig.Config{}
				if ignores {
					config.Ignores = repoconfig.IgnoreConfigs{{Rule: "probe", Step: "remote"}}
				}
				f.options.Bindings = testBindings(config)
				f.options.CollectScripts = collect
				f.options.BindSource = testSourceBindings(func(path string) (*repoconfig.Config, error) {
					t.Errorf("opaque call loaded an action config: %s", path)
					return &repoconfig.Config{}, nil
				})
				f.options.Rules = append(f.options.Rules, &ignoreProbeRule{Base: workflowvisit.NewBase("probe", "")})
				result, err := Workflow(f.options, f.root)
				if err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, finding := range result.Findings {
					if finding.Rule != "probe" {
						continue
					}
					found++
					if (finding.IgnoreOrigin != nil) != ignores {
						t.Fatalf("root opaque call lost entry ignores: %+v", finding)
					}
					if origin := finding.IgnoreOrigin; origin != nil && (origin.StepID != "remote" || origin.Uses != "owner/action@v1") {
						t.Fatalf("root opaque call has foreign selectors: %+v", origin)
					}
				}
				if found != 1 || len(result.Batch.Scopes()) != 1 || result.Batch.Scopes()[0].Config != config {
					t.Fatalf("opaque call created a child scope/config or lost its finding: %+v", result)
				}
			})
		}
	}
}
