package main

import "actionlint.kjanat.dev/v2/internal/model/rule"

var exampleFollowups = map[string]rule.Example{
	"script-interpolation": {
		Description: "To log untrusted text, also stop workflow commands around the output. The EXIT trap resumes commands if printing fails.",
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
