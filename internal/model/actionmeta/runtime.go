package actionmeta

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

type Runtime struct {
	Removed    bool
	Deprecated bool
	// An empty ExecutionRuntime means the runner uses runs.using.
	ExecutionRuntime string
	DeprecationURL   string
	RemovalDate      string
}

func ValidRuntimes() string {
	names := append([]string{"composite", "docker"}, slices.Sorted(maps.Keys(Runtimes))...)
	for i, name := range names {
		names[i] = strconv.Quote(name)
	}
	return strings.Join(names, ", ")
}

type LifecycleContext struct {
	Service services.Service
	AsOf    lifecycle.Date
}

type RuntimeFinding struct {
	Code     ProblemCode
	Message  string
	Evidence *lifecycle.Evidence
}

// RuntimeProblem retains future evidence for notification policy even when Current is false.
func RuntimeProblem(using string, context LifecycleContext) RuntimeFinding {
	if strings.EqualFold(using, "node20") && (context.Service.Kind() == services.GitHubCom || context.Service.Kind() == services.GitHubDataResidency) && !context.AsOf.IsZero() {
		effective, _ := lifecycle.ParseDate("2026-09-23")
		announced, _ := lifecycle.ParseDate("2026-08-25")
		// The August 25 amendment establishes this deadline.
		evidence := &lifecycle.Evidence{Subject: lifecycle.ActionRuntimes, Name: "node20", Kind: lifecycle.SupportEnds, Service: context.Service, Announced: announced, Effective: effective, AsOf: context.AsOf, Source: "https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/", Current: context.AsOf.Compare(effective) >= 0}
		message := fmt.Sprintf("runtime %q is deprecated; support for executing actions with Node 20 on %s ends on %s; actions execute with Node 24 instead", using, context.Service.Kind(), effective)
		if evidence.Current {
			message = fmt.Sprintf("runtime %q is deprecated; %s runs JavaScript actions with Node 24 and no longer supports the Node 20 opt-out as of %s", using, context.Service.Kind(), effective)
		}
		return RuntimeFinding{CodeRuntimeDeprecated, message, evidence}
	}
	r, ok := Runtimes[strings.ToLower(using)]
	if !ok || !r.Removed && !r.Deprecated {
		return RuntimeFinding{}
	}
	evidence := &lifecycle.Evidence{Subject: lifecycle.ActionRuntimes, Name: strings.ToLower(using), Kind: lifecycle.RuntimeReplaced, Service: context.Service, AsOf: context.AsOf, Current: true, Source: "https://github.com/actions/runner/tree/cf65f3ec8100ee0046d735ac5d647e00f4b8e817"}
	if r.ExecutionRuntime != "" && !strings.EqualFold(using, r.ExecutionRuntime) {
		return RuntimeFinding{CodeRuntimeDeprecated, fmt.Sprintf("runtime %q is deprecated; current GitHub Actions runners use %q instead", using, r.ExecutionRuntime), evidence}
	}
	evidence.Kind = lifecycle.SupportEnds
	if r.DeprecationURL != "" {
		evidence.Source = r.DeprecationURL
	}
	if r.RemovalDate != "" {
		evidence.Effective, _ = lifecycle.ParseDate(r.RemovalDate)
	}
	if r.Removed {
		return RuntimeFinding{CodeRuntimeRemoved, fmt.Sprintf("runtime %q is no longer bundled with current GitHub Actions runners", using), evidence}
	}
	message := fmt.Sprintf("runtime %q is deprecated in GitHub Actions", using)
	if r.RemovalDate != "" {
		message += "; removal is scheduled for " + r.RemovalDate
	}
	return RuntimeFinding{CodeRuntimeDeprecated, message + ". see " + r.DeprecationURL, evidence}
}
