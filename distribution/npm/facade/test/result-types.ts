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
	RelatedLocation,
	SourceRevision,
} from '@kjanat/actionlint/result';

type AssertFalse<T extends false> = T;
type LocationFields = {
	message: string;
	path: string;
	start: { line: number; column: number };
	end: { line: number; column: number };
};
export type RevisionAloneRejected = AssertFalse<
	LocationFields & { revision: SourceRevision } extends RelatedLocation ? true : false
>;
export type CoordinatesAloneRejected = AssertFalse<
	LocationFields & { coordinates: 'yaml' } extends RelatedLocation ? true : false
>;
export const boundRelated: RelatedLocation = {
	message: 'declaration',
	path: 'ci.yml',
	start: { line: 1, column: 1 },
	end: { line: 1, column: 1 },
	revision: { document: '/repo/ci.yml', digest: '0'.repeat(64) },
	coordinates: 'yaml',
};
export const unboundRelated: RelatedLocation = {
	message: 'declaration',
	path: 'ci.yml',
	start: { line: 1, column: 1 },
	end: { line: 1, column: 1 },
};

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
