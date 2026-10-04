/** Public analysis result v1. Additive fields do not change schema_version. */
export type Position = { line: number; column: number };
export type Edit = { path: string; start: Position; end: Position; replacement: string };
export type Fix = { description: string; edits: Edit[] };
export type Diagnostic = {
	rule: string;
	message: string;
	path: string;
	start: Position;
	end: Position;
	snippet?: string;
	severity?: string;
	code?: string;
	fixes?: Fix[];
};
export type ConfigOrigin = { source: string; state: string; input?: string; line?: number; column?: number };
export type ConfigWarning = { message: string; line: number; column: number };
export type ResultConfig = {
	file: string;
	project: string;
	overrides: string[] | null;
	origins?: Record<string, ConfigOrigin>;
	warnings?: ConfigWarning[];
};

/** Parsed workflow structure, not a full syntax tree or execution plan. */
export type WorkflowOutline = {
	path: string;
	name?: string;
	parse_status: 'complete' | 'partial' | 'failed';
	triggers: string[];
	jobs: JobOutline[];
};
export type JobOutline = {
	id: string;
	name?: string;
	start?: Position;
	needs: string[];
	uses?: string;
	steps: StepOutline[];
};
export type StepOutline = {
	id?: string;
	name?: string;
	start?: Position;
	/** Known kinds: run, uses, wait, cancel, parallel, unknown. */
	kind: string;
	uses?: string;
	/** Children of a parallel step, in declaration order. */
	steps?: StepOutline[];
};

export type ResultData = {
	schema_version: 1;
	file_count: number | null;
	diagnostics: Diagnostic[];
	configurations: ResultConfig[];
	hints: string[];
	workflows?: WorkflowOutline[];
	sarif?: Record<string, unknown>;
	error?: string;
};

export type CheckResultV1 =
	& ResultData
	& (
		| { completed: true; status: 'success'; exit_code: 0 }
		| { completed: true; status: 'problems-found'; exit_code: 1 }
		| { completed: false; status: 'invalid-options'; exit_code: 2 }
		| { completed: false; status: 'failure'; exit_code: 3 }
	);

/** Result emitted by the matching npm package version. */
export type CheckResult = CheckResultV1;

/** JSONL record; completion status is conveyed separately. */
export type DiagnosticRecord = Diagnostic & { schema_version: 1 };
