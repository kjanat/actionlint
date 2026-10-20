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
	rule := validator{}
	rule.checkDockerAction(exec.Uses.Value, exec)
	return rule.errs
}

// InvalidReference reports an invalid action reference at its original declaration.
func InvalidReference(pos *workflowlang.Pos, spec, why string) *findingmodel.Finding {
	rule := validator{}
	rule.invalidActionFormat(pos, spec, why)
	return rule.errs[0]
}

func (rule *validator) checkRepoAction(spec string, exec *workflowlang.ExecAction, lookup func(string) (*actionmeta.Metadata, bool), log func(string, ...any)) {
	debug := func(format string, args ...any) {
		if log != nil {
			log(format, args...)
		}
	}
	owner, repo, ref, problem := references.SplitAction(spec)
	if problem != "" {
		rule.invalidActionFormat(exec.Uses.Pos, spec, problem)
		return
	}

	if owner == "" || repo == "" || ref == "" {
		rule.invalidActionFormat(exec.Uses.Pos, spec, "owner and repo and ref should not be empty")
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

func (rule *validator) invalidActionFormat(pos *workflowlang.Pos, spec string, why string) {
	rule.Errorf(pos, "specifying action %q in invalid format because %s. available formats are \"{owner}/{repo}@{ref}\", \"{owner}/{repo}/{path}@{ref}\", \"./{path}\", or \"$/{path}\"", spec, why)
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-the-github-packages-container-registry
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *validator) checkDockerAction(uri string, exec *workflowlang.ExecAction) {
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
			exec.Uses.Pos,
			"URI for Docker container %q is invalid: %s (tag=%s)",
			uri,
			err.Error(),
			tag,
		)
	}

	if tagExists && tag == "" {
		rule.Errorf(exec.Uses.Pos, "tag of Docker action should not be empty: %q", uri)
	}
}
