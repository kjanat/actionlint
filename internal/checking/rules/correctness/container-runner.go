package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ContainerRunnerDeclaration describes the container-runner rule.
func ContainerRunnerDeclaration() rule.Declaration {
	return rule.Declaration{Name: "container-runner", Description: "Report Docker container requirements for selected runners", Category: "correctness", Subcategory: "runners", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "GitHub-managed job containers, service containers and Docker actions require Linux and a Docker daemon. Report an error for known Windows or macOS selections. Warn for GitHub-hosted ubuntu-slim: it includes Docker client tools but no local daemon; a compatible remote daemon may satisfy the requirement. Inspect available action metadata and expanded composites, with matrix rows and conditions determining which selections can reach the container. Unknown runner facts and unavailable action bodies do not establish a violation. Docker commands in scripts can use remote daemons and are not classified by this rule.", Help: "Select a Linux runner with Docker, such as ubuntu-latest, or guard the container action so incompatible matrix rows do not execute it. For ubuntu-slim, verify any remote daemon configuration before suppressing the warning. Self-hosted Linux runners need an accessible Docker daemon. No automatic fix is offered because changing the runner can change job behavior.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#container-runner", Examples: []rule.Example{{
		Description: "Use a Linux runner with Docker for a job container.",
		Before:      "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: windows-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
		After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    container: alpine:3\n    steps:\n      - run: echo ok\n",
	}}, SupportsActions: true, Scope: rule.CompositeConfiguration}
}
