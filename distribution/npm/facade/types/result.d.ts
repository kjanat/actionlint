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

/** Selected declarations observed by the workflow parser. */
export type WorkflowOutline = {
	kind: 'workflow';
	path: string;
	name?: string;
	/** Parser outcome and recovery; validity is reported separately in diagnostics. */
	parse_status: 'complete' | 'partial' | 'failed';
	triggers: string[];
	jobs: JobOutline[];
	description?: never;
	inputs?: never;
	outputs?: never;
	runs?: never;
};
/** Selected declarations observed by the action metadata decoder. */
export type ActionOutline = {
	kind: 'action';
	path: string;
	name?: string;
	description?: string;
	/** Decoder outcome and recovery; complete does not guarantee valid or executable metadata. */
	parse_status: 'complete' | 'partial' | 'failed';
	inputs: ActionInputOutline[];
	outputs: ActionOutputOutline[];
	runs: ActionRunsOutline;
	triggers?: never;
	jobs?: never;
};
/** Lossy report projection; parser checks differ between workflow and action documents. */
export type DocumentOutline = WorkflowOutline | ActionOutline;
export type ActionInputOutline = {
	id: string;
	description?: string;
	required?: boolean;
	default?: string;
	start?: Position;
};
export type ActionOutputOutline = { id: string; description?: string; value?: string; start?: Position };
/** Observed runtime declarations, with no guarantee of runner support or executability. */
export type ActionRunsOutline =
	| { kind: 'composite'; steps: StepOutline[] }
	| { kind: 'javascript'; using: string; main?: string; pre?: string; post?: string; pre_if?: string; post_if?: string }
	| {
		kind: 'docker';
		image?: string;
		entrypoint?: string;
		pre_entrypoint?: string;
		post_entrypoint?: string;
		args: string[];
	}
	| { kind: 'plugin'; plugin: string }
	| { kind: 'unknown'; using?: string };
/** Syntactic classification; host resolution, dialect support and provider identity remain separate. */
export type UsesReference =
	| {
		kind: 'repository';
		owner: string;
		repo: string;
		subpath: string;
		ref: string;
		host?: string;
		scheme?: string;
		host_source: 'default' | 'explicit' | 'self';
	}
	| { kind: 'workspace' | 'self-repository'; path: string }
	| { kind: 'container'; image: string }
	| { kind: 'builtin'; name: string }
	| { kind: 'unknown' };
export type JobOutline = {
	id: string;
	name?: string;
	start?: Position;
	needs: string[];
	uses?: string;
	reference?: UsesReference;
	steps: StepOutline[];
};
export type StepOutline = {
	id?: string;
	name?: string;
	start?: Position;
	/** Known kinds: run, uses, wait, cancel, parallel, unknown. */
	kind: string;
	uses?: string;
	reference?: UsesReference;
	/** Children of a parallel step, in declaration order. */
	steps?: StepOutline[];
};

export type ResultData = {
	schema_version: 1;
	file_count: number | null;
	diagnostics: Diagnostic[];
	configurations: ResultConfig[];
	hints: string[];
	documents?: DocumentOutline[];
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
