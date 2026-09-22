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

type ResultData = {
	schema_version: 1;
	file_count: number | null;
	diagnostics: Diagnostic[];
	configurations: unknown[];
	hints: string[];
	sarif?: Record<string, unknown>;
	error?: string;
};

export type ActionResult =
	& ResultData
	& (
		| { completed: true; status: 'success'; exit_code: 0 }
		| { completed: true; status: 'problems-found'; exit_code: 1 }
		| { completed: false; status: 'invalid-options'; exit_code: 2 }
		| { completed: false; status: 'failure'; exit_code: 3 }
	);

export function object(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function integer(value: unknown, minimum: number): value is number {
	return typeof value === 'number' && Number.isSafeInteger(value) && value >= minimum;
}

function position(value: unknown): value is Position {
	return object(value) && integer(value.line, 1) && integer(value.column, 1);
}

function span(start: Position, end: Position): boolean {
	return end.line > start.line || (end.line === start.line && end.column >= start.column);
}

function edit(value: unknown): value is Edit {
	return object(value) && typeof value.path === 'string' && typeof value.replacement === 'string'
		&& position(value.start) && position(value.end) && span(value.start, value.end);
}

function fix(value: unknown): value is Fix {
	return object(value) && typeof value.description === 'string' && Array.isArray(value.edits)
		&& value.edits.length > 0 && value.edits.every(edit);
}

function diagnostic(value: unknown): value is Diagnostic {
	return object(value) && typeof value.rule === 'string' && typeof value.message === 'string'
		&& typeof value.path === 'string' && position(value.start) && position(value.end) && span(value.start, value.end)
		&& (value.snippet === undefined || typeof value.snippet === 'string')
		&& (value.severity === undefined || typeof value.severity === 'string')
		&& (value.code === undefined || typeof value.code === 'string')
		&& (value.fixes === undefined || (Array.isArray(value.fixes) && value.fixes.every(fix)));
}

function sarif(value: unknown): value is Record<string, unknown> {
	return object(value) && value.version === '2.1.0' && Array.isArray(value.runs) && value.runs.every(object);
}

export function parseResult(value: unknown): ActionResult {
	if (
		!object(value) || value.schema_version !== 1
		|| (value.file_count !== null && !integer(value.file_count, 0))
		|| !Array.isArray(value.diagnostics) || !value.diagnostics.every(diagnostic)
		|| !Array.isArray(value.configurations) || !Array.isArray(value.hints)
		|| !value.hints.every((hint) => typeof hint === 'string')
		|| (value.sarif !== undefined && !sarif(value.sarif))
		|| (value.error !== undefined && typeof value.error !== 'string')
	) throw new Error('actionlint returned an invalid or unsupported persisted result');
	const data: ResultData = {
		schema_version: 1,
		file_count: value.file_count,
		diagnostics: value.diagnostics,
		configurations: value.configurations,
		hints: value.hints,
	};
	if (value.sarif !== undefined) data.sarif = value.sarif;
	if (value.error !== undefined) data.error = value.error;
	if (
		value.completed === true && value.status === 'success' && value.exit_code === 0 && value.diagnostics.length === 0
	) {
		return { ...data, completed: true, status: 'success', exit_code: 0 };
	}
	if (
		value.completed === true && value.status === 'problems-found' && value.exit_code === 1
		&& value.diagnostics.length > 0
	) {
		return { ...data, completed: true, status: 'problems-found', exit_code: 1 };
	}
	if (value.completed === false && value.status === 'invalid-options' && value.exit_code === 2) {
		return { ...data, completed: false, status: 'invalid-options', exit_code: 2 };
	}
	if (value.completed === false && value.status === 'failure' && value.exit_code === 3) {
		return { ...data, completed: false, status: 'failure', exit_code: 3 };
	}
	throw new Error('actionlint returned inconsistent analysis status');
}

export function failedResult(error: unknown, invalid = false): ActionResult {
	const data: ResultData = {
		schema_version: 1,
		file_count: null,
		diagnostics: [],
		configurations: [],
		hints: [],
		error: error instanceof Error ? error.message : String(error),
	};
	return invalid
		? { ...data, completed: false, status: 'invalid-options', exit_code: 2 }
		: { ...data, completed: false, status: 'failure', exit_code: 3 };
}
