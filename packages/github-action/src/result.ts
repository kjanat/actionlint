import type {
	ActionInputOutline,
	ActionOutputOutline,
	ActionRunsOutline,
	CheckResult,
	ConfigOrigin,
	ConfigWarning,
	Diagnostic,
	DocumentOutline,
	Edit,
	Fix,
	JobOutline,
	Position,
	ResultConfig,
	ResultData,
	StepOutline,
	UsesReference,
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

function absent(value: Record<string, unknown>, fields: readonly string[]): boolean {
	return fields.every((field) => !(field in value));
}

function optionalStrings(value: Record<string, unknown>, fields: readonly string[]): boolean {
	return fields.every((field) => value[field] === undefined || typeof value[field] === 'string');
}

function usesReference(value: unknown): value is UsesReference {
	if (!object(value)) return false;
	const fields = ['owner', 'repo', 'subpath', 'ref', 'host', 'scheme', 'host_source', 'path', 'image', 'name'];
	const only = (allowed: readonly string[]) => absent(value, fields.filter((field) => !allowed.includes(field)));
	switch (value.kind) {
		case 'repository':
			return only(['owner', 'repo', 'subpath', 'ref', 'host', 'scheme', 'host_source'])
				&& ['owner', 'repo', 'subpath', 'ref'].every((field) => typeof value[field] === 'string')
				&& optionalStrings(value, ['host', 'scheme'])
				&& (value.host_source === 'default' || value.host_source === 'explicit' || value.host_source === 'self');
		case 'workspace':
		case 'self-repository':
			return only(['path']) && typeof value.path === 'string';
		case 'container':
			return only(['image']) && typeof value.image === 'string';
		case 'builtin':
			return only(['name']) && typeof value.name === 'string';
		case 'unknown':
			return only([]);
		default:
			return false;
	}
}

function stepOutline(value: unknown): value is StepOutline {
	return object(value) && typeof value.kind === 'string'
		&& (value.id === undefined || typeof value.id === 'string')
		&& (value.name === undefined || typeof value.name === 'string')
		&& (value.start === undefined || position(value.start))
		&& (value.uses === undefined || typeof value.uses === 'string')
		&& (value.reference === undefined || usesReference(value.reference))
		&& (value.steps === undefined || (Array.isArray(value.steps) && value.steps.every(stepOutline)));
}

function jobOutline(value: unknown): value is JobOutline {
	return object(value) && typeof value.id === 'string' && strings(value.needs)
		&& (value.name === undefined || typeof value.name === 'string')
		&& (value.start === undefined || position(value.start))
		&& (value.uses === undefined || typeof value.uses === 'string')
		&& (value.reference === undefined || usesReference(value.reference))
		&& Array.isArray(value.steps) && value.steps.every(stepOutline);
}

function actionInput(value: unknown): value is ActionInputOutline {
	return object(value) && typeof value.id === 'string' && optionalStrings(value, ['description', 'default'])
		&& (value.required === undefined || typeof value.required === 'boolean')
		&& (value.start === undefined || position(value.start));
}

function actionOutput(value: unknown): value is ActionOutputOutline {
	return object(value) && typeof value.id === 'string' && optionalStrings(value, ['description', 'value'])
		&& (value.start === undefined || position(value.start));
}

function actionRuns(value: unknown): value is ActionRunsOutline {
	if (!object(value)) return false;
	const fields = [
		'using',
		'steps',
		'main',
		'pre',
		'post',
		'pre_if',
		'post_if',
		'image',
		'entrypoint',
		'pre_entrypoint',
		'post_entrypoint',
		'args',
		'plugin',
	];
	const only = (allowed: readonly string[]) => absent(value, fields.filter((field) => !allowed.includes(field)));
	switch (value.kind) {
		case 'composite':
			return only(['steps']) && Array.isArray(value.steps) && value.steps.every(stepOutline);
		case 'javascript':
			return only(['using', 'main', 'pre', 'post', 'pre_if', 'post_if']) && typeof value.using === 'string'
				&& optionalStrings(value, ['main', 'pre', 'post', 'pre_if', 'post_if']);
		case 'docker':
			return only(['image', 'entrypoint', 'pre_entrypoint', 'post_entrypoint', 'args']) && strings(value.args)
				&& optionalStrings(value, ['image', 'entrypoint', 'pre_entrypoint', 'post_entrypoint']);
		case 'plugin':
			return only(['plugin']) && typeof value.plugin === 'string';
		case 'unknown':
			return only(['using']) && optionalStrings(value, ['using']);
		default:
			return false;
	}
}

function documentOutline(value: unknown): value is DocumentOutline {
	if (
		!object(value) || typeof value.path !== 'string' || !optionalStrings(value, ['name'])
		|| (value.parse_status !== 'complete' && value.parse_status !== 'partial' && value.parse_status !== 'failed')
	) return false;
	if (value.kind === 'workflow') {
		return absent(value, ['description', 'inputs', 'outputs', 'runs']) && strings(value.triggers)
			&& Array.isArray(value.jobs) && value.jobs.every(jobOutline);
	}
	return value.kind === 'action' && absent(value, ['triggers', 'jobs']) && optionalStrings(value, ['description'])
		&& Array.isArray(value.inputs) && value.inputs.every(actionInput)
		&& Array.isArray(value.outputs) && value.outputs.every(actionOutput) && actionRuns(value.runs);
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
		|| (value.documents !== undefined && (!Array.isArray(value.documents) || !value.documents.every(documentOutline)))
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
	if (value.documents !== undefined) data.documents = value.documents;
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
		documents: [],
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
