package testutil

import (
	"context"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	actionfixtures "actionlint.kjanat.dev/v2/internal/development/tests/integration/actions/fixtures"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

// WithFixtureKnowledge supplies the corpus's explicit host evidence. Configured
// repository labels still override the known-empty additional-label inventory.
func WithFixtureKnowledge(t *testing.T, options checking.Options) checking.Options {
	t.Helper()
	if options.RemoteActions == nil {
		options.RemoteActions = FixtureActions(t)
	}
	if options.Repository == nil {
		labels, err := repository.NewNames()
		if err != nil {
			t.Fatal(err)
		}
		options.Repository = func(context.Context, string) repository.Snapshot {
			return repository.Snapshot{RunnerLabels: labels}
		}
	}
	return options
}

type fixtureActionKey struct{ host, repository, directory, ref string }

func actionFixtureKey(ref actions.Reference) fixtureActionKey {
	return fixtureActionKey{ref.Host(), strings.ToLower(ref.Repository()), ref.Directory(), ref.Ref()}
}

type fixtureManifest struct {
	identity actions.Identity
	object   string
	route    actions.Route
	filename string
	content  string
}

type fixtureActions struct {
	manifests map[fixtureActionKey]fixtureManifest
}

func (p *fixtureActions) Lookup(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return actions.Manifest{}, err
	}
	manifest, ok := p.manifests[actionFixtureKey(ref)]
	if !ok {
		return actions.UnavailableManifest(availability.NotFetched()), nil
	}
	selection, err := actions.NewSelection(ref, manifest.identity, manifest.object, manifest.route, "")
	if err != nil {
		return actions.Manifest{}, err
	}
	return actions.NewManifestFile(selection, manifest.filename, []byte(manifest.content))
}

func fixtureRepositoryID(fields map[string]string) (repositoryid.ID, error) {
	text, present := fields["repository-id"]
	if !present {
		return repositoryid.ID{}, nil
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return repositoryid.ID{}, err
	}
	return repositoryid.Known(value)
}

// FixtureActions reads local manifests with their provenance in comment headers.
func FixtureActions(t *testing.T) actions.Provider {
	t.Helper()
	const directory = "testdata/remote-actions"
	entries, err := actionfixtures.Manifests.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fixtureActions{manifests: make(map[fixtureActionKey]fixtureManifest)}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}
		data, err := actionfixtures.ReadManifest(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		header, content, found := strings.Cut(string(data), "\n---")
		if !found || !strings.HasPrefix(content, "\n") && !strings.HasPrefix(content, "\r\n") {
			t.Fatalf("fixture %s needs a comment header followed by ---", entry.Name())
		}
		content = strings.TrimPrefix(strings.TrimPrefix(content, "\r"), "\n")
		fields := map[string]string{}
		for line := range strings.SplitSeq(header, "\n") {
			if key, value, ok := strings.Cut(strings.TrimPrefix(line, "# "), ": "); ok {
				fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		ref, err := actions.ParseReference("github.com", fields["uses"])
		if err != nil {
			t.Fatal(err)
		}
		source, err := url.Parse(fields["source"])
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(strings.TrimPrefix(source.Path, "/"), "/", 5)
		if source.Scheme != "https" || source.Host != "github.com" || len(parts) != 5 || parts[2] != "blob" {
			t.Fatalf("fixture %s needs a commit-pinned source URL", entry.Name())
		}
		id, err := fixtureRepositoryID(fields)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := actions.NewIdentity(ref.Host(), parts[0]+"/"+parts[1], id, parts[3], ref.Directory())
		if err != nil {
			t.Fatal(err)
		}
		route, err := actions.RefRoute(fields["ref"])
		if err != nil {
			t.Fatal(err)
		}
		object := fields["object"]
		if object == "" {
			object = identity.Commit()
		}
		selection, err := actions.NewSelection(ref, identity, object, route, "")
		if err != nil {
			t.Fatal(err)
		}
		filename := path.Base(parts[4])
		if _, err := actions.NewManifestFile(selection, filename, []byte(content)); err != nil {
			t.Fatal(err)
		}
		key := actionFixtureKey(ref)
		if _, exists := provider.manifests[key]; exists {
			t.Fatalf("duplicate manifest fixture %s", fields["uses"])
		}
		provider.manifests[key] = fixtureManifest{identity, object, route, filename, content}
	}
	if len(provider.manifests) == 0 {
		t.Fatal("no action manifest fixtures found")
	}
	return provider
}
