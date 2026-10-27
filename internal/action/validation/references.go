package validation

import (
	"net/url"
	"strconv"
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// Repository checks a repository action reference and the metadata selected for it.
// The caller supplies catalogue lookup and optional debug logging.
func Repository(exec *workflowlang.ExecAction, lookup func(string) (*actionmeta.Metadata, bool), log func(string, ...any)) []*findingmodel.Finding {
	if exec == nil || exec.Uses == nil {
		return nil
	}
	rule := validator{}
	rule.checkRepoAction(exec.Uses.Value, exec, lookup, log)
	return rule.errs
}

// Docker checks the URI and optional tag of a Docker action reference.
func Docker(exec *workflowlang.ExecAction) []*findingmodel.Finding {
	if exec == nil || exec.Uses == nil || !strings.HasPrefix(exec.Uses.Value, "docker://") {
		return nil
	}
	return DockerReference(exec.Uses.Value, exec.Uses.Pos)
}

// DockerReference validates a Docker reference from its canonical source value.
func DockerReference(spec string, pos *workflowlang.Pos) []*findingmodel.Finding {
	return DockerReferenceValue(spec, positionValue(pos), pos != nil)
}

// DockerReferenceValue validates a Docker reference from an owned scalar value and position.
func DockerReferenceValue(spec string, pos workflowlang.Pos, hasPosition bool) []*findingmodel.Finding {
	if !strings.HasPrefix(spec, "docker://") {
		return nil
	}
	rule := validator{}
	rule.checkDockerAction(spec, positionPointer(pos, hasPosition))
	return rule.errs
}

// RepositoryReference validates repository reference syntax without consulting metadata.
func RepositoryReference(spec string, pos *workflowlang.Pos) []*findingmodel.Finding {
	return RepositoryReferenceValue(spec, positionValue(pos), pos != nil)
}

// RepositoryReferenceValue validates reference syntax from an owned scalar value and position.
func RepositoryReferenceValue(spec string, pos workflowlang.Pos, hasPosition bool) []*findingmodel.Finding {
	rule := validator{}
	rule.checkRepositoryReference(spec, positionPointer(pos, hasPosition))
	return rule.errs
}

// InvalidActionReference uses the same syntax checks as Repository and Docker validation.
func InvalidActionReference(spec string, pos workflowlang.Pos, hasPosition bool) bool {
	switch {
	case strings.HasPrefix(spec, "./"):
		return false
	case strings.HasPrefix(spec, "$/"):
		_, valid := references.SelfRepositoryLocal(spec)
		return !valid
	case strings.HasPrefix(spec, "docker://"):
		return len(DockerReferenceValue(spec, pos, hasPosition)) != 0
	default:
		return len(RepositoryReferenceValue(spec, pos, hasPosition)) != 0
	}
}

func positionValue(pos *workflowlang.Pos) workflowlang.Pos {
	if pos == nil {
		return workflowlang.Pos{}
	}
	return *pos
}

func positionPointer(pos workflowlang.Pos, present bool) *workflowlang.Pos {
	if !present {
		return nil
	}
	return &pos
}

// InvalidReference reports an invalid action reference at its original declaration.
func InvalidReference(pos *workflowlang.Pos, spec, why string) *findingmodel.Finding {
	rule := validator{}
	rule.invalidActionFormat(pos, spec, why)
	return rule.errs[0]
}

func (rule *validator) checkRepoAction(spec string, exec *workflowlang.ExecAction, lookup func(string) (*actionmeta.Metadata, bool), log func(string, ...any)) {
	if !rule.checkRepositoryReference(spec, exec.Uses.Pos) {
		return
	}
	debug := func(format string, args ...any) {
		if log != nil {
			log(format, args...)
		}
	}
	var meta *actionmeta.Metadata
	retired := false
	if lookup != nil {
		meta, retired = lookup(spec)
	}
	if meta == nil {
		if retired {
			rule.Errorf(exec.Uses.Pos, "the runtime or service used by %q action is retired on GitHub.com. update the action's version to fix this issue", spec)
			return
		}
		debug("This action is not found in popular actions data set: %s", spec)
		return
	}
	if problem := actionmeta.RuntimeProblem(meta.Runs.Using); problem != "" {
		rule.Errorf(exec.Uses.Pos, "%s. update the version of action %q", problem, spec)
	}
	if meta.SkipInputs {
		debug("This action skips to check inputs: %s", spec)
		return
	}

	rule.errs = append(rule.errs, Inputs(meta, exec, func(m *actionmeta.Metadata) string {
		return strconv.Quote(spec)
	})...)
}

func (rule *validator) checkRepositoryReference(spec string, pos *workflowlang.Pos) bool {
	owner, repo, ref, problem := references.SplitAction(spec)
	if problem != "" {
		rule.invalidActionFormat(pos, spec, problem)
		return false
	}
	if owner == "" || repo == "" || ref == "" {
		rule.invalidActionFormat(pos, spec, "owner and repo and ref should not be empty")
		return false
	}
	return true
}

func (rule *validator) invalidActionFormat(pos *workflowlang.Pos, spec string, why string) {
	rule.Errorf(pos, "specifying action %q in invalid format because %s. available formats are \"{owner}/{repo}@{ref}\", \"{owner}/{repo}/{path}@{ref}\", \"./{path}\", or \"$/{path}\"", spec, why)
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-the-github-packages-container-registry
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *validator) checkDockerAction(uri string, pos *workflowlang.Pos) {
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
			"URI for Docker container %q is invalid: %s (tag=%s)",
			uri,
			err.Error(),
			tag,
		)
	}

	if tagExists && tag == "" {
		rule.Errorf(pos, "tag of Docker action should not be empty: %q", uri)
	}
}
