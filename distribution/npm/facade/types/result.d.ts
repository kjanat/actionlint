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

export type ResultData = {
	schema_version: 1;
	file_count: number | null;
	diagnostics: Diagnostic[];
	configurations: ResultConfig[];
	hints: string[];
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
