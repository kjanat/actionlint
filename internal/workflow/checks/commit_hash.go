package checks

import (
	"regexp"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/references"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// reCommitSHA matches a full-length Git object ID in both the SHA-1 and the SHA-256 object formats.
// https://git-scm.com/docs/hash-function-transition
var reCommitSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// reImageDigest matches the "digest" part of an image reference in the OCI grammar, which is
// "digest-algorithm ':' digest-hex" with at least 32 hexadecimal digits.
// https://github.com/distribution/reference
var reImageDigest = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)

type commitHashCheck struct {
	findingCollector
	excluded func(string) bool
}

func CommitHashStep(step *workflowlang.Step, excluded func(string) bool) []Finding {
	rule := &commitHashCheck{name: "require-commit-hash", excluded: excluded}
	rule.checkStep(step)
	return rule.out
}

func CommitHashJob(job *workflowlang.Job, excluded func(string) bool) []Finding {
	rule := &commitHashCheck{name: "require-commit-hash", excluded: excluded}
	rule.checkJob(job)
	return rule.out
}

func (rule *commitHashCheck) checkStep(n *workflowlang.Step) {
	e, ok := n.Exec.(*workflowlang.ExecAction)
	if !ok || e.Uses == nil || e.Uses.ContainsExpression() || (rule.excluded != nil && rule.excluded(e.Uses.Value)) {
		return
	}

	if image, ok := strings.CutPrefix(e.Uses.Value, "docker://"); ok {
		rule.checkImageDigest(image, e.Uses)
		return
	}

	ref, ok := actionUsesRepoRef(e.Uses.Value)
	if !ok || reCommitSHA.MatchString(ref) {
		return
	}

	rule.Errorf(
		e.Uses.Pos,
		"the ref %q of action %q is not a commit SHA. actions must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"require-commit-hash\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		e.Uses.Value,
	)
}

func (rule *commitHashCheck) checkImageDigest(image string, uses *workflowlang.String) {
	if strings.HasSuffix(image, ":") {
		return
	}

	_, digest, ok := strings.Cut(image, "@")
	if ok && reImageDigest.MatchString(digest) {
		return
	}

	rule.Errorf(
		uses.Pos,
		"the Docker image %q is not pinned to an image digest. images must be specified in \"docker://{image}@{algorithm}:{hex}\" form because \"require-commit-hash\" is enabled in the \"policy\" configuration",
		uses.Value,
	)
}

func (rule *commitHashCheck) checkJob(n *workflowlang.Job) {
	if n.WorkflowCall == nil {
		return
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() || (rule.excluded != nil && rule.excluded(u.Value)) {
		return
	}

	ref, ok := references.WorkflowRef(u.Value)
	if !ok || reCommitSHA.MatchString(ref) {
		return
	}

	rule.Errorf(
		u.Pos,
		"the ref %q of reusable workflow %q is not a commit SHA. reusable workflow calls must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"require-commit-hash\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		u.Value,
	)
}

// Return the {ref} part of a {owner}/{repo}@{ref} or {owner}/{repo}/{path}@{ref} specification. The
// second return value is false when the specification has another form. RuleAction reports the forms
// which are invalid.
func actionUsesRepoRef(spec string) (string, bool) {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") {
		return "", false
	}
	owner, repo, ref, problem := references.SplitAction(spec)
	if problem != "" || owner == "" || repo == "" || ref == "" {
		return "", false
	}
	return ref, true
}
