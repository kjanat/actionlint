import type {
	CheckResult,
	ConfigOrigin,
	ConfigWarning,
	Diagnostic,
	Edit,
	Fix,
	JobOutline,
	Position,
	ResultConfig,
	ResultData,
	StepOutline,
	WorkflowOutline,
} from '../../../distribution/npm/facade/types/result.d.ts';

export type { Diagnostic, Edit, Fix, Position } from '../../../distribution/npm/facade/types/result.d.ts';
export type ActionResult = CheckResult;

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

function configOrigin(value: unknown): value is ConfigOrigin {
	return object(value) && typeof value.source === 'string' && typeof value.state === 'string'
		&& (value.input === undefined || typeof value.input === 'string')
		&& (value.line === undefined || integer(value.line, 0))
		&& (value.column === undefined || integer(value.column, 0));
}

function configWarning(value: unknown): value is ConfigWarning {
	return object(value) && typeof value.message === 'string' && integer(value.line, 0) && integer(value.column, 0);
}

function configuration(value: unknown): value is ResultConfig {
	return object(value) && typeof value.file === 'string' && typeof value.project === 'string'
		&& (value.overrides === null
			|| (Array.isArray(value.overrides) && value.overrides.every((entry) => typeof entry === 'string')))
		&& (value.origins === undefined || (object(value.origins) && Object.values(value.origins).every(configOrigin)))
		&& (value.warnings === undefined || (Array.isArray(value.warnings) && value.warnings.every(configWarning)));
}

function strings(value: unknown): value is string[] {
	return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

function stepOutline(value: unknown): value is StepOutline {
	return object(value) && typeof value.kind === 'string'
		&& (value.id === undefined || typeof value.id === 'string')
		&& (value.name === undefined || typeof value.name === 'string')
		&& (value.start === undefined || position(value.start))
		&& (value.uses === undefined || typeof value.uses === 'string')
		&& (value.steps === undefined || (Array.isArray(value.steps) && value.steps.every(stepOutline)));
}

function jobOutline(value: unknown): value is JobOutline {
	return object(value) && typeof value.id === 'string' && strings(value.needs)
		&& (value.name === undefined || typeof value.name === 'string')
		&& (value.start === undefined || position(value.start))
		&& (value.uses === undefined || typeof value.uses === 'string')
		&& Array.isArray(value.steps) && value.steps.every(stepOutline);
}

function workflowOutline(value: unknown): value is WorkflowOutline {
	return object(value) && typeof value.path === 'string'
		&& (value.name === undefined || typeof value.name === 'string')
		&& (value.parse_status === 'complete' || value.parse_status === 'partial' || value.parse_status === 'failed')
		&& strings(value.triggers) && Array.isArray(value.jobs) && value.jobs.every(jobOutline);
}

export function parseResult(value: unknown): ActionResult {
	if (
		!object(value) || value.schema_version !== 1
		|| (value.file_count !== null && !integer(value.file_count, 0))
		|| !Array.isArray(value.diagnostics) || !value.diagnostics.every(diagnostic)
		|| !Array.isArray(value.configurations) || !value.configurations.every(configuration) || !Array.isArray(value.hints)
		|| !value.hints.every((hint) => typeof hint === 'string')
		|| (value.sarif !== undefined && !sarif(value.sarif))
		|| (value.error !== undefined && typeof value.error !== 'string')
		|| (value.workflows !== undefined && (!Array.isArray(value.workflows) || !value.workflows.every(workflowOutline)))
	) throw new Error('actionlint returned an invalid or unsupported persisted result');
	const data: ResultData = {
		// Schema v1 allows additive fields; retain them when reporters serialize the result.
		...value,
		schema_version: 1,
		file_count: value.file_count,
		diagnostics: value.diagnostics,
		configurations: value.configurations,
		hints: value.hints,
	};
	if (value.sarif !== undefined) data.sarif = value.sarif;
	if (value.error !== undefined) data.error = value.error;
	if (value.workflows !== undefined) data.workflows = value.workflows;
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
