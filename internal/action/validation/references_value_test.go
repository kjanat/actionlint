package validation

import (
	"reflect"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

func TestOwnedReferenceValidationMatchesLegacyFindings(t *testing.T) {
	position := workflowlang.Pos{Line: 7, Col: 11}
	for _, spec := range []string{"owner/repo@v1", "owner/repo", "./child", "$/child", "$/", "docker://ghcr.io/team/image", "docker://ghcr.io/team/image:"} {
		t.Run(spec, func(t *testing.T) {
			legacyRepository := RepositoryReference(spec, &position)
			ownedRepository := RepositoryReferenceValue(spec, position, true)
			if !reflect.DeepEqual(legacyRepository, ownedRepository) {
				t.Fatalf("repository findings differ: legacy=%v owned=%v", legacyRepository, ownedRepository)
			}
			legacyDocker := DockerReference(spec, &position)
			ownedDocker := DockerReferenceValue(spec, position, true)
			if !reflect.DeepEqual(legacyDocker, ownedDocker) {
				t.Fatalf("Docker findings differ: legacy=%v owned=%v", legacyDocker, ownedDocker)
			}
			wantInvalid := false
			switch {
			case len(spec) >= 2 && spec[:2] == "./":
			case len(spec) >= 2 && spec[:2] == "$/":
				_, valid := references.SelfRepositoryLocal(spec)
				wantInvalid = !valid
			case len(spec) >= len("docker://") && spec[:len("docker://")] == "docker://":
				wantInvalid = len(ownedDocker) != 0
			default:
				wantInvalid = len(ownedRepository) != 0
			}
			if got := InvalidActionReference(spec, position, true); got != wantInvalid {
				t.Fatalf("invalid reference=%v, want %v", got, wantInvalid)
			}
		})
	}
}

func TestOwnedReferenceValidationPreservesAbsentPosition(t *testing.T) {
	if got := RepositoryReferenceValue("owner/repo@v1", workflowlang.Pos{}, false); got != nil {
		t.Fatalf("valid reference without source position findings = %v, want nil", got)
	}
	findings := RepositoryReferenceValue("owner/repo", workflowlang.Pos{}, true)
	if findings == nil {
		t.Fatal("invalid reference unexpectedly returned nil findings")
	}
}
