package actionlint

import (
	"strings"
	"testing"
)

func TestPopularActionsDataset(t *testing.T) {
	if len(PopularActions) == 0 {
		t.Fatal("popular actions data set is empty")
	}

	for n, meta := range PopularActions {
		if meta == nil {
			t.Fatalf("metadata for %s is nil", n)
		}
		if meta.Name == "" {
			t.Fatalf("action name for %s is empty", n)
		}
		for id, i := range meta.Inputs {
			if id != strings.ToLower(id) {
				t.Errorf("input ID %q is not in lower case at %q", id, n)
			}
			if i.Name == "" {
				t.Errorf("input name is not empty at ID %q at %q", id, n)
			}
		}
		for id, o := range meta.Outputs {
			if id != strings.ToLower(id) {
				t.Errorf("output ID %q is not in lower case at %q", id, n)
			}
			if o.Name == "" {
				t.Errorf("output name is not empty at ID %q at %q", id, n)
			}
		}
	}
}

func TestPopularActionsCurrentMajors(t *testing.T) {
	for _, spec := range []string{
		"actions/cache@v6",
		"actions/checkout@v7",
		"actions/setup-go@v7",
		"codecov/codecov-action@v7",
		"dawidd6/action-download-artifact@v23",
	} {
		if _, ok := PopularActions[spec]; !ok {
			t.Errorf("popular action %q is missing", spec)
		}
	}
}

func TestExpandedPopularActions(t *testing.T) {
	for _, spec := range []string{
		"oven-sh/setup-bun@v2",
		"astral-sh/setup-uv@v7",
		"pnpm/action-setup@v6",
		"denoland/setup-deno@v2",
		"docker/bake-action@v7",
		"dependabot/fetch-metadata@v3",
		"gradle/actions/setup-gradle@v6",
		"gradle/actions/dependency-submission@v6",
		"gradle/actions/wrapper-validation@v6",
		"snyk/actions/node@v1",
		"googleapis/release-please-action@v5",
		"EndBug/add-and-commit@v11",
		"Platane/snk@v3",
		"aquasecurity/trivy-action@v0.36.0",
		"github/codeql-action/upload-sarif@v4",
		"jdx/mise-action@v5",
		"anchore/sbom-action/download-syft@v0",
		"autofix-ci/action@v1",
		"cachix/install-nix-action@v31",
		"KSXGitHub/github-actions-deploy-aur@v3",
		"peter-evans/dockerhub-description@v5",
		"immanuwell/dockerfile-roast@1.7.0",
		"kjanat/install-dprint@v2",
		"kjanat/runner@v0.28.0",
		"reviewdog/action-ansiblelint@v1",
		"EPMatt/reviewdog-action-prettier@v1",
		"dciborow/action-pylint@0.1.1",
		"tsuyoshicho/action-textlint@v3",
		"dotenv-linter/action-dotenv-linter@v3",
		"ilammy/msvc-dev-cmd@v1",
		"vale-cli/vale-action@v3",
		"codemagic-ci-cd/trigger-codemagic-workflow-action@v3.0.1",
	} {
		if meta := PopularActions[spec]; meta == nil || len(meta.Inputs) == 0 {
			t.Errorf("expected generated input metadata for %q, got %+v", spec, meta)
		}
	}
	for _, spec := range []string{"actions/setup-ruby@v1", "actions/setup-elixir@v1", "actions/upload-release-asset@v1"} {
		if _, ok := OutdatedPopularActionSpecs[spec]; !ok {
			t.Errorf("expected retired runtime classification for %q", spec)
		}
	}
}
