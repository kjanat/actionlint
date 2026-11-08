package rule

import "strings"

// LabeledWorkflowExample adds source labels to a before-and-after workflow example.
func LabeledWorkflowExample(description, beforeLabel, afterLabel, before, after string) Example {
	e := WorkflowExample(description, before, after)
	e.Before = "# " + beforeLabel + "\n" + e.Before
	e.After = "# " + afterLabel + "\n" + e.After
	return e
}

// WorkflowExample places before-and-after job fields in complete workflows.
func WorkflowExample(description, before, after string) Example {
	return Example{Description: description, Before: WorkflowExampleSource(before), After: WorkflowExampleSource(after)}
}

// WorkflowExampleSource wraps job fields in a workflow and supplies a step when absent.
func WorkflowExampleSource(job string) string {
	source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n" + job
	for line := range strings.SplitSeq(job, "\n") {
		if strings.HasPrefix(line, "    steps:") {
			return source
		}
	}
	source += "    steps:\n      - run: echo ok\n"
	return source
}
