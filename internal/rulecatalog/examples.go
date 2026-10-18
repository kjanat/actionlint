package rulecatalog

import (
	"actionlint.kjanat.dev/rule"
	"strings"
)

// Examples are executable workflow documents. Engine tests check that the before
// form reports the named rule and the after form is clean without external tools.
var examples = map[string][]rule.Example{
	"script-injection": {
		labeledExample("Prefer the tool's native environment variable: GitHub CLI reads GH_TOKEN directly, keeping the secret out of script text and command arguments. GitHub secrets are automatically masked in logs; register generated or fetched secrets with add-mask before use. Masking does not protect process arguments.",
			"wrong: secret in script text", "correct: secret in environment variable",
			"    steps:\n      - run: GH_TOKEN=\"${{ secrets.TOKEN }}\" gh api user\n",
			"    steps:\n      - env:\n          GH_TOKEN: ${{ secrets.TOKEN }}\n        run: gh api user\n"),
		labeledExample("When supported, pass secrets through stdin. Bash's printf builtin feeds Docker's password-stdin option without putting the password in Docker's arguments. GitHub masks this stored secret automatically; generated or fetched values must be masked before use. Keep shell tracing disabled.",
			"wrong: password in command arguments", "correct: password through stdin",
			"    steps:\n      - env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n        run: docker login --username \"$REGISTRY_USER\" --password \"${{ secrets.REGISTRY_PASSWORD }}\"\n",
			"    steps:\n      - shell: bash\n        env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n          REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}\n        run: printf '%s' \"${REGISTRY_PASSWORD}\" | docker login --username \"${REGISTRY_USER}\" --password-stdin\n"),
	},
	"script-interpolation": {labeledExample("Keep a dynamic value out of generated shell source.",
		"wrong: template value in script text", "better: template value in environment variable",
		"    steps:\n      - run: echo \"${{ vars.MESSAGE }}\"\n",
		"    steps:\n      - env: { MESSAGE: \"${{ vars.MESSAGE }}\" }\n        run: echo \"${MESSAGE}\"\n")},
	"env-var": {example("Use environment names without spaces.",
		"    env:\n      BAD NAME: value\n", "    env:\n      GOOD_NAME: value\n")},
	"id": {example("Begin step IDs with a letter or underscore.",
		"    steps:\n      - id: 1build\n        run: echo ok\n", "    steps:\n      - id: build\n        run: echo ok\n")},
	"job-needs": {example("Remove a dependency on a job that does not exist.",
		"    needs: missing\n", "")},
	"permissions": {example("Choose an allowed access level for the permission scope.",
		"    permissions:\n      contents: execute\n", "    permissions:\n      contents: read\n")},
	"if-cond": {example("Remove an unconditional truthy condition.",
		"    if: true\n", "")},
	"credentials": {example("Read container credentials from a secret.",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: plaintext\n",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: ${{ secrets.REGISTRY_PASSWORD }}\n")},
	"shell-name": {example("Correct the shell name.",
		"    steps:\n      - shell: bashh\n        run: echo ok\n", "    steps:\n      - shell: bash\n        run: echo ok\n")},
}

// Additional documentation examples build on the rule's before/after pair.
var exampleFollowups = map[string]rule.Example{
	"script-interpolation": {
		Description: "Best when logging untrusted text: keep the environment binding and suspend workflow-command processing around the output. Use a fresh unpredictable token for each invocation, keep shell tracing disabled, and register any secret masks before stopping commands. The EXIT trap resumes commands even if printing fails. This protects the log command channel; it does not sanitize shell source, hide secrets, or escape terminal control characters. If raw text is unnecessary, use a serializer or escaping routine for the intended output format instead of ad hoc replacements. See [GitHub's stop-commands guidance](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-commands#stopping-and-starting-workflow-commands).",
		After: "# best: untrusted log output cannot issue workflow commands\n" + exampleWorkflow(`    steps:
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

func labeledExample(description, beforeLabel, afterLabel, before, after string) rule.Example {
	e := example(description, before, after)
	e.Before = "# " + beforeLabel + "\n" + e.Before
	e.After = "# " + afterLabel + "\n" + e.After
	return e
}

func example(description, before, after string) rule.Example {
	return rule.Example{Description: description, Before: exampleWorkflow(before), After: exampleWorkflow(after)}
}

func exampleWorkflow(job string) string {
	source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n" + job
	if !strings.Contains(job, "    steps:\n") {
		source += "    steps:\n      - run: echo ok\n"
	}
	return source
}
