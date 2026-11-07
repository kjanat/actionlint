package security

import (
	"regexp"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// UnpinnedReferenceCheck binds reference exclusions to job and step declarations.
func UnpinnedReferenceCheck(excluded func(string) bool) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: UnpinnedReferenceDeclaration(),
		Job:         func(job declaration.JobView) []reporting.Finding { return CommitHashJob(job, excluded) },
		Step:        func(step declaration.StepView) []reporting.Finding { return CommitHashStep(step, excluded) },
	}
}

// reCommitSHA matches a full-length Git object ID in both the SHA-1 and the SHA-256 object formats.
// https://git-scm.com/docs/hash-function-transition
var reCommitSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// reImageDigest matches the "digest" part of an image reference in the OCI grammar, which is
// "digest-algorithm ':' digest-hex" with at least 32 hexadecimal digits.
// https://github.com/distribution/reference
var reImageDigest = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)

type commitHashCheck struct {
	reporting.Collector
	excluded func(string) bool
}

func CommitHashStep(step declaration.StepView, excluded func(string) bool) []reporting.Finding {
	rule := &commitHashCheck{Rule: "unpinned-reference", excluded: excluded}
	rule.checkStep(step)
	return rule.Findings
}

func CommitHashJob(job declaration.JobView, excluded func(string) bool) []reporting.Finding {
	rule := &commitHashCheck{Rule: "unpinned-reference", excluded: excluded}
	rule.checkJob(job)
	return rule.Findings
}

func (rule *commitHashCheck) checkStep(n declaration.StepView) {
	e, ok := n.Action()
	if !ok || !e.Uses().Present() || e.Uses().ContainsExpression() || (rule.excluded != nil && rule.excluded(e.Uses().Value())) {
		return
	}

	if image, ok := strings.CutPrefix(e.Uses().Value(), "docker://"); ok {
		rule.checkImageDigest(image, e.Uses())
		return
	}

	ref, ok := actionUsesRepoRef(e.Uses().Value())
	if !ok || reCommitSHA.MatchString(ref) {
		return
	}

	rule.Errorf(
		reporting.ViewPosition(e.Uses().Pos()),
		"the ref %q of action %q is not a commit SHA. actions must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"unpinned-reference\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		e.Uses().Value(),
	)
}

func (rule *commitHashCheck) checkImageDigest(image string, uses declaration.StringView) {
	if strings.HasSuffix(image, ":") {
		return
	}

	_, digest, ok := strings.Cut(image, "@")
	if ok && reImageDigest.MatchString(digest) {
		return
	}

	rule.Errorf(
		reporting.ViewPosition(uses.Pos()),
		"the Docker image %q is not pinned to an image digest. images must be specified in \"docker://{image}@{algorithm}:{hex}\" form because \"unpinned-reference\" is enabled in the \"policy\" configuration",
		uses.Value(),
	)
}

func (rule *commitHashCheck) checkJob(n declaration.JobView) {
	if !n.WorkflowCall().Present() {
		return
	}

	u := n.WorkflowCall().Uses()
	if !u.Present() || u.Value() == "" || u.ContainsExpression() || (rule.excluded != nil && rule.excluded(u.Value())) {
		return
	}

	ref, ok := references.WorkflowRef(u.Value())
	if !ok || reCommitSHA.MatchString(ref) {
		return
	}

	rule.Errorf(
		reporting.ViewPosition(u.Pos()),
		"the ref %q of reusable workflow %q is not a commit SHA. reusable workflow calls must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"unpinned-reference\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		u.Value(),
	)
}

// Return the {ref} part of a {owner}/{repo}@{ref} or {owner}/{repo}/{path}@{ref} specification. The
// second return value is false when the specification has another form. The "action" rule reports the forms
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
