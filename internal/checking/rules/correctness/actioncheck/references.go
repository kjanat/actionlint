package actioncheck

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Repository checks a repository action reference against available metadata.
func Repository(exec *definition.ExecAction, metadata actionmeta.Definition, context actionmeta.LifecycleContext) []*finding.Finding {
	if exec == nil || exec.Uses == nil {
		return nil
	}
	rule := validator{lifecycle: context}
	rule.checkRepoAction(exec.Uses.Value, exec, metadata)
	return rule.errs
}

// Docker checks the URI and optional tag of a Docker action reference.
func Docker(exec *definition.ExecAction) []*finding.Finding {
	if exec == nil || exec.Uses == nil || callreferences.FormOf(exec.Uses.Value) != callreferences.FormContainer {
		return nil
	}
	return DockerReference(exec.Uses.Value, exec.Uses.Pos)
}

// DockerReference validates a Docker reference from its canonical source value.
func DockerReference(spec string, pos *sourcetext.Position) []*finding.Finding {
	return DockerReferenceValue(spec, sourcetext.ZeroIfNil(pos), pos != nil)
}

// DockerReferenceValue validates a Docker reference from an owned scalar value and position.
func DockerReferenceValue(spec string, pos sourcetext.Position, hasPosition bool) []*finding.Finding {
	if callreferences.FormOf(spec) != callreferences.FormContainer {
		return nil
	}
	rule := validator{}
	rule.checkDockerAction(spec, sourcetext.NilUnlessPresent(pos, hasPosition))
	return rule.errs
}

// RepositoryReference validates repository reference syntax without consulting metadata.
func RepositoryReference(spec string, pos *sourcetext.Position) []*finding.Finding {
	return RepositoryReferenceValue(spec, sourcetext.ZeroIfNil(pos), pos != nil)
}

// RepositoryReferenceValue validates reference syntax from an owned scalar value and position.
func RepositoryReferenceValue(spec string, pos sourcetext.Position, hasPosition bool) []*finding.Finding {
	rule := validator{}
	rule.checkRepositoryReference(spec, sourcetext.NilUnlessPresent(pos, hasPosition))
	return rule.errs
}

// InvalidActionReference uses the same syntax checks as Repository and Docker validation.
func InvalidActionReference(spec string, pos sourcetext.Position, hasPosition bool) bool {
	switch callreferences.FormOf(spec) {
	case callreferences.FormWorkspace:
		return false
	case callreferences.FormSelfRepository:
		_, valid := callreferences.SelfRepositoryLocal(spec)
		return !valid
	case callreferences.FormContainer:
		return len(DockerReferenceValue(spec, pos, hasPosition)) != 0
	case callreferences.FormRepository, callreferences.FormBuiltin:
	}
	return len(RepositoryReferenceValue(spec, pos, hasPosition)) != 0
}

// InvalidReference reports an invalid action reference at its original declaration.
func InvalidReference(pos *sourcetext.Position, spec, why string) *finding.Finding {
	rule := validator{}
	rule.invalidActionFormat(pos, spec, why)
	return rule.errs[0]
}

func (rule *validator) checkRepoAction(spec string, exec *definition.ExecAction, meta actionmeta.Definition) {
	if !rule.checkRepositoryReference(spec, exec.Uses.Pos) {
		return
	}
	if !meta.Present() {
		return
	}
	if problem := actionmeta.RuntimeProblem(meta.Using(), rule.lifecycle); problem.Code != "" {
		rule.runtimeFinding(exec.Uses.Pos, problem, fmt.Sprintf("update the version of action %q", spec))
	}
	rule.checkAction(meta, exec, strconv.Quote(spec))
}

func (rule *validator) runtimeFinding(pos *sourcetext.Position, problem actionmeta.RuntimeFinding, help string) {
	rule.Error(pos, problem.Code, problem.Message+". "+help)
	finding := rule.errs[len(rule.errs)-1]
	finding.LifecycleEvidence = problem.Evidence
	if problem.Evidence != nil {
		finding.DocumentationURL = problem.Evidence.Source
	}
}

func (rule *validator) checkRepositoryReference(spec string, pos *sourcetext.Position) bool {
	_, problem := callreferences.ParseRepositoryAction(spec)
	if problem != "" {
		rule.invalidActionFormat(pos, spec, problem)
		return false
	}
	return true
}

func (rule *validator) invalidActionFormat(pos *sourcetext.Position, spec string, why string) {
	rule.Errorf(pos, actionmeta.CodeInvalidReference, "specifying action %q in invalid format because %s. available formats are \"{owner}/{repo}@{ref}\", \"{owner}/{repo}/{path}@{ref}\", \"./{path}\", or \"$/{path}\"", spec, why)
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-the-github-packages-container-registry
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *validator) checkDockerAction(uri string, pos *sourcetext.Position) {
	tag := ""
	tagExists := false
	if idx := strings.IndexRune(uri[len("docker://"):], ':'); idx != -1 {
		idx += len("docker://")
		if idx < len(uri) {
			tag = uri[idx+1:]
			uri = uri[:idx]
			tagExists = true
		}
	}

	if _, err := url.Parse(uri); err != nil {
		rule.Errorf(
			pos,
			actionmeta.CodeInvalidReference,
			"URI for Docker container %q is invalid: %s (tag=%s)",
			uri,
			err.Error(),
			tag,
		)
	}

	if tagExists && tag == "" {
		rule.Errorf(pos, actionmeta.CodeInvalidReference, "tag of Docker action should not be empty: %q", uri)
	}
}
