package checking

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type mirrorRequests struct {
	sync.Mutex
	actions, workflows map[references.MirrorAccess]int
}

type mirrorProvider struct {
	requests *mirrorRequests
	access   references.MirrorAccess
}

func (p mirrorProvider) WithMetadataMirrors(access references.MirrorAccess, _ []string) actions.Provider {
	p.access = access
	return p
}

func (p mirrorProvider) WithWorkflowMetadataMirrors(access references.MirrorAccess, _ []string) workflows.Provider {
	p.access = access
	return p
}

func (p mirrorProvider) Lookup(context.Context, actions.Reference) (actions.Manifest, error) {
	p.requests.Lock()
	defer p.requests.Unlock()
	p.requests.actions[p.access]++
	return actions.UnavailableManifest(availability.Inaccessible()), nil
}

func (p mirrorProvider) LookupWorkflow(context.Context, string) (workflows.RemoteFile, error) {
	p.requests.Lock()
	defer p.requests.Unlock()
	p.requests.workflows[p.access]++
	return workflows.UnavailableRemoteFile(availability.Inaccessible()), nil
}

func TestMetadataMirrorConfigurationSeparatesLookupCaches(t *testing.T) {
	t.Parallel()
	requests := &mirrorRequests{actions: map[references.MirrorAccess]int{}, workflows: map[references.MirrorAccess]int{}}
	provider := mirrorProvider{requests: requests}
	request := analysisRequest{WorkingDir: t.TempDir(), RemoteActions: provider, RemoteWorkflows: provider}
	const source = `on: push
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: example/action@v1
  call:
    uses: example/workflows/.github/workflows/build.yml@v1
`
	for i, setting := range []string{`{}`, `{"allow-unknown-repositories":true}`, `{"services":[]}`} {
		cfg, err := ConfigurationRules().Parse([]byte(`{"network":{"metadata-mirrors":` + setting + `}}`))
		if err != nil {
			t.Fatal(err)
		}
		for j := range 2 {
			request.Sources = append(request.Sources, sourceUnit{Path: fmt.Sprintf("workflow-%d-%d.yml", i, j), Content: []byte(source), Config: cfg})
		}
	}
	if _, err := runTestAnalysis(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	for _, access := range []references.MirrorAccess{references.PublicMirrorsOnly, references.AllowUnknownMirrors, references.NoMirrors} {
		if requests.actions[access] != 1 || requests.workflows[access] != 1 {
			t.Errorf("access %v: action lookups = %d, workflow lookups = %d; want one each for the two files sharing this setting", access, requests.actions[access], requests.workflows[access])
		}
	}
}
