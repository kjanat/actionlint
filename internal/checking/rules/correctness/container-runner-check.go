package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
)

// ContainerRunnerFindings reports established container limitations. Source
// acquisition, condition evaluation and invocation state belong to the host.
func ContainerRunnerFindings(uses []runnerlang.ContainerUse) []reporting.Finding {
	var findings []reporting.Finding
	for _, use := range uses {
		var description string
		switch use.Kind {
		case runnerlang.JobContainer:
			description = "job container"
		case runnerlang.ServiceContainer:
			description = "service " + use.Name
		case runnerlang.DockerAction:
			description = fmt.Sprintf("Docker action %q", use.Name)
		}
		var message, severity string
		switch use.Target.Limitation {
		case runnerlang.ContainerRequiresLinux:
			message = fmt.Sprintf("%s requires a Linux runner with Docker. Runner selection %q resolves to a non-Linux runner", description, use.Target.Runner)
			severity = "error"
		case runnerlang.ContainerMissingDaemon:
			message = fmt.Sprintf("%s requires a Docker daemon. Runner selection %q has Docker client tools but cannot run containers locally. Configure a compatible remote daemon or select a Docker-capable Linux runner", description, use.Target.Runner)
			severity = "warning"
		case runnerlang.ContainerUnknown:
			continue
		}
		findings = append(findings, reporting.Finding{Rule: "container-runner", Message: message, Severity: severity, Start: use.Position})
	}
	return findings
}
