package github

import (
	"context"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type repositoryFile struct {
	reference         actions.Reference
	identity          actions.Identity
	selection         actions.Selection
	observedAt        string
	filename, content string
	missing           availability.Unavailability
}

func unavailableFile(reason availability.Unavailability) repositoryFile {
	return repositoryFile{missing: reason}
}
func (f repositoryFile) Available() bool            { return f.identity.Commit() != "" }
func (f repositoryFile) Identity() actions.Identity { return f.identity }
func (f repositoryFile) Content() string            { return f.content }
func (f repositoryFile) Filename() string           { return f.filename }

// LookupWorkflow uses workflow reference selection and the same HTTP client,
// request accounting and immutable cache checks as action metadata.
func (g *GitHub) LookupWorkflow(ctx context.Context, spec string) (workflows.RemoteFile, error) {
	if !workflows.IsRemoteReference(spec) {
		return workflows.RemoteFile{}, nil
	}
	ref, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return workflows.RemoteFile{}, err
	}
	client := *g
	if client.cacheDir != "" {
		client.cacheDir = filepath.Join(client.cacheDir, "workflows")
	}
	file, err := client.lookupFile(ctx, ref, "workflow", []string{""})
	if err != nil {
		return workflows.RemoteFile{}, err
	}
	if !file.Available() {
		return workflows.UnavailableRemoteFile(file.missing), nil
	}
	return workflows.NewRemoteFile(spec, file.Identity().Commit(), []byte(file.Content()))
}
