// Compile against the package export, as an npm consumer would.
import type {
	ActionRunsOutline,
	CheckResult,
	CheckResultV1,
	DiagnosticRecord,
	DocumentOutline,
	JobOutline,
	StepOutline,
	UsesReference,
	WorkflowOutline,
} from '@kjanat/actionlint/result';

export function completedStatus(result: CheckResult): 0 | 1 | undefined {
	if (result.completed) return result.exit_code;
	return undefined;
}

export function failureStatus(result: CheckResultV1): 2 | 3 | undefined {
	if (!result.completed) return result.exit_code;
	return undefined;
}

export function recordVersion(record: DiagnosticRecord): 1 {
	return record.schema_version;
}

export function workflowJobs(workflow: WorkflowOutline): JobOutline[] {
	return workflow.jobs;
}

export function jobSteps(job: JobOutline): StepOutline[] {
	return job.steps;
}

export function documentOutlines(result: CheckResult): DocumentOutline[] {
	return result.documents ?? [];
}

export function runtimeSteps(runs: ActionRunsOutline): StepOutline[] {
	return runs.kind === 'composite' ? runs.steps : [];
}

export function referenceHost(reference: UsesReference): string | undefined {
	return reference.kind === 'repository' ? reference.host : undefined;
}
