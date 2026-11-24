package actioncheck

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
)

func TestReferenceValueMatchesPointerFindings(t *testing.T) {
	t.Parallel()
	position := sourcetext.Position{Line: 7, Column: 11}
	for _, spec := range []string{"owner/repo@v1", "owner/repo", "./child", "$/child", "$/", "$/child@v1", "builtin:name", "self:owner/repo@v1", "docker://ghcr.io/team/image", "docker://ghcr.io/team/image:"} {
		t.Run(spec, func(t *testing.T) {
			t.Parallel()
			legacyRepository := RepositoryReference(spec, &position)
			valueRepository := RepositoryReferenceValue(spec, position, true)
			if !reflect.DeepEqual(legacyRepository, valueRepository) {
				t.Fatalf("repository findings differ: legacy=%v value=%v", legacyRepository, valueRepository)
			}
			legacyDocker := DockerReference(spec, &position)
			valueDocker := DockerReferenceValue(spec, position, true)
			if !reflect.DeepEqual(legacyDocker, valueDocker) {
				t.Fatalf("Docker findings differ: legacy=%v value=%v", legacyDocker, valueDocker)
			}
			wantInvalid := false
			switch {
			case len(spec) >= 2 && spec[:2] == "./":
			case len(spec) >= 2 && spec[:2] == "$/":
				_, valid := callreferences.SelfRepositoryLocal(spec)
				wantInvalid = !valid
			case len(spec) >= len("docker://") && spec[:len("docker://")] == "docker://":
				wantInvalid = len(valueDocker) != 0
			default:
				wantInvalid = len(valueRepository) != 0
			}
			if got := InvalidActionReference(spec, position, true); got != wantInvalid {
				t.Fatalf("invalid reference=%v, want %v", got, wantInvalid)
			}
		})
	}
}

func TestReferenceValueWithZeroPosition(t *testing.T) {
	t.Parallel()
	if got := RepositoryReferenceValue("owner/repo@v1", sourcetext.Position{}, false); got != nil {
		t.Fatalf("valid reference without source position findings = %v, want nil", got)
	}
	findings := RepositoryReferenceValue("owner/repo", sourcetext.Position{}, true)
	if findings == nil {
		t.Fatal("invalid reference unexpectedly returned nil findings")
	}
}
