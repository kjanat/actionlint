package rulecatalog

import (
	"strings"

	"actionlint.kjanat.dev/v2/rule"
)

// Examples are executable workflow documents. Engine tests check that the before
// form reports the named rule and the after form is clean without external tools.
var examples = map[string][]rule.Example{
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
