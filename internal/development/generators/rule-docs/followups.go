package main

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// Additional documentation examples build on the rule's before/after pair.
var exampleFollowups = map[string]rule.Example{
	"script-interpolation": {
		Description: "Best when logging untrusted text: keep the environment binding and suspend workflow-command processing around the output. Use a fresh unpredictable token for each invocation, keep shell tracing disabled, and register any secret masks before stopping commands. The EXIT trap resumes commands even if printing fails. This protects the log command channel; it does not sanitize shell source, hide secrets, or escape terminal control characters. If raw text is unnecessary, use a serializer or escaping routine for the intended output format. See [GitHub's stop-commands guidance](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-commands#stopping-and-starting-workflow-commands).",
		After: "# best: untrusted log output cannot issue workflow commands\n" + rule.WorkflowExampleSource(`    steps:
      - shell: bash
        env: { MESSAGE: "${{ vars.MESSAGE }}" }
        run: |
          stop_token="$(openssl rand -hex 32)"
          trap 'printf "::%s::\n" "${stop_token}"' EXIT
          printf '::stop-commands::%s\n' "${stop_token}"
          printf '%s\n' "${MESSAGE}"
`),
	},
}
