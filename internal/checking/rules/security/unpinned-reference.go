package security

import (
	"regexp"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// UnpinnedReferenceRule is the single definition of the unpinned-reference rule.
var UnpinnedReferenceRule = &define.Rule{
	Name:        "unpinned-reference",
	Aliases:     []rule.Alias{{Name: "require-commit-hash", Deprecated: true}},
	Description: "Require full commit SHAs for actions and workflows, and digests for images",
	Category:    "security",
	Subcategory: "dependencies",
	Help:        "Pin uses references to a full commit SHA and container images to a digest.",
	Options:     define.Options[lintconfig.CommitHashOptions](),
	Checks: func(c define.Inputs) define.Checks {
		options := new(lintconfig.CommitHashOptions)
		if c.Config != nil {
			// Parsing and ValidateRuleNames reject options that do not load
			// here; without that check they leave no exclusions.
			setting, _ := c.Config.Lint.Rules.Policy.Rule("unpinned-reference")
			if _, err := setting.LoadOptions(options); err != nil {
				options = new(lintconfig.CommitHashOptions)
			}
		}
		return UnpinnedReferenceChecks(options.Excludes)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.RequiresCommitHash() },
}

// UnpinnedReferenceChecks binds reference exclusions to job and step declarations.
func UnpinnedReferenceChecks(excluded func(string) bool) define.Checks {
	return define.Checks{
		Job:  func(job declaration.Job) []reporting.Finding { return CommitHashJob(job, excluded) },
		Step: func(step declaration.Step) []reporting.Finding { return CommitHashStep(step, excluded) },
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

func CommitHashStep(step declaration.Step, excluded func(string) bool) []reporting.Finding {
	rule := &commitHashCheck{excluded: excluded}
	rule.checkStep(step)
	return rule.Findings
}

func CommitHashJob(job declaration.Job, excluded func(string) bool) []reporting.Finding {
	rule := &commitHashCheck{excluded: excluded}
	rule.checkJob(job)
	return rule.Findings
}

func (rule *commitHashCheck) checkStep(n declaration.Step) {
	e, ok := n.Action()
	if !ok || e.Uses == nil || e.Uses.ContainsExpression() || (rule.excluded != nil && rule.excluded(e.Uses.Value)) {
		return
	}

	if image, ok := references.ContainerImage(e.Uses.Value); ok {
		rule.checkImageDigest(image, e.Uses)
		return
	}

	ref, ok := actionUsesRepoRef(e.Uses.Value)
	if !ok || reCommitSHA.MatchString(ref) {
		return
	}

	rule.Errorf(
		e.Uses.Pos,
		"the ref %q of action %q is not a commit SHA. actions must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits). see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
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
		"the Docker image %q is not pinned to an image digest. images must be specified in \"docker://{image}@{algorithm}:{hex}\" form",
		uses.Value,
	)
}

func (rule *commitHashCheck) checkJob(n declaration.Job) {
	if !n.Present() || n.WorkflowCall == nil {
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
		"the ref %q of reusable workflow %q is not a commit SHA. reusable workflow calls must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits). see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		u.Value,
	)
}

// Return the {ref} part of a {owner}/{repo}@{ref} or {owner}/{repo}/{path}@{ref} specification. The
// second return value is false when the specification has another form. The "action" rule reports the forms
// which are invalid.
func actionUsesRepoRef(spec string) (string, bool) {
	if form := references.FormOf(spec); form == references.FormWorkspace || form == references.FormSelfRepository {
		return "", false
	}
	owner, repo, ref, problem := references.SplitAction(spec)
	if problem != "" || owner == "" || repo == "" || ref == "" {
		return "", false
	}
	return ref, true
}
