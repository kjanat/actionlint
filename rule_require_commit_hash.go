package actionlint

import (
	"regexp"
	"strings"
)

// reCommitSHA matches a full-length Git object ID in both the SHA-1 and the SHA-256 object formats.
// https://git-scm.com/docs/hash-function-transition
var reCommitSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// reImageDigest matches the "digest" part of an image reference in the OCI grammar, which is
// "digest-algorithm ':' digest-hex" with at least 32 hexadecimal digits.
// https://github.com/distribution/reference
var reImageDigest = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[0-9a-fA-F]{32,}$`)

// RuleRequireCommitHash is a rule to check that every "uses:" is pinned to something immutable, which
// is a full-length commit SHA for an action or a reusable workflow and an image digest for a Docker
// image. The "require-commit-hash" policy in the configuration file enables it.
type RuleRequireCommitHash struct {
	RuleBase
}

// NewRuleRequireCommitHash creates a new RuleRequireCommitHash instance.
func NewRuleRequireCommitHash() *RuleRequireCommitHash {
	return &RuleRequireCommitHash{
		RuleBase: RuleBase{
			name: "require-commit-hash",
			desc: "Checks that every \"uses:\" is pinned to a full-length commit SHA or an image digest",
		},
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleRequireCommitHash) VisitStep(n *Step) error {
	e, ok := n.Exec.(*ExecAction)
	if !ok || e.Uses == nil || e.Uses.ContainsExpression() {
		return nil
	}

	if image, ok := strings.CutPrefix(e.Uses.Value, "docker://"); ok {
		rule.checkImageDigest(image, e.Uses)
		return nil
	}

	ref, ok := actionUsesRepoRef(e.Uses.Value)
	if !ok || reCommitSHA.MatchString(ref) {
		return nil
	}

	rule.Errorf(
		e.Uses.Pos,
		"the ref %q of action %q is not a commit SHA. actions must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"require-commit-hash\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		e.Uses.Value,
	)
	return nil
}

func (rule *RuleRequireCommitHash) checkImageDigest(image string, uses *String) {
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

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleRequireCommitHash) VisitJobPre(n *Job) error {
	if n.WorkflowCall == nil {
		return nil
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return nil
	}

	ref, ok := workflowCallUsesRepoRef(u.Value)
	if !ok || reCommitSHA.MatchString(ref) {
		return nil
	}

	rule.Errorf(
		u.Pos,
		"the ref %q of reusable workflow %q is not a commit SHA. reusable workflow calls must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because \"require-commit-hash\" is enabled in the \"policy\" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details",
		ref,
		u.Value,
	)
	return nil
}

// Return the {ref} part of a {owner}/{repo}@{ref} or {owner}/{repo}/{path}@{ref} specification. The
// second return value is false when the specification has another form. RuleAction reports the forms
// which are invalid.
func actionUsesRepoRef(spec string) (string, bool) {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") {
		return "", false
	}
	owner, repo, ref, problem := splitActionUses(spec)
	if problem != "" || owner == "" || repo == "" || ref == "" {
		return "", false
	}
	return ref, true
}
