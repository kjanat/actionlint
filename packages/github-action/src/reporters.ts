import { appendFile, mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { ActionResult, Diagnostic } from '#result';
import { failedResult, parseResult } from '#result';
import { postReview } from '#review';
import type { Environment } from '#runtime';
import { InputError } from '#runtime';
import { commandEscape, writeOutputs } from '#workflow';

export type ReportOptions = { annotations: boolean; summary: boolean; json: boolean; sarif: boolean; review: boolean };

export function reportOptions(environment: Environment): ReportOptions {
	const enabled = (name: string): boolean => {
		const value = environment[`INPUT_${name.toUpperCase()}`] || 'false';
		if (value !== 'true' && value !== 'false') throw new InputError(`Input '${name}' must be 'true' or 'false'`);
		return value === 'true';
	};
	const formats = (environment['INPUT_REPORT-FORMATS'] || '').split(/[\s,]+/).filter(Boolean);
	if (formats.some((format) => format !== 'json' && format !== 'sarif')) {
		throw new InputError("Input 'report-formats' accepts json and sarif, separated by commas or newlines");
	}
	return {
		annotations: enabled('annotations'),
		summary: enabled('summary'),
		review: enabled('review'),
		json: formats.includes('json'),
		sarif: formats.includes('sarif'),
	};
}

function property(value: string): string {
	return commandEscape(value).replaceAll(':', '%3A').replaceAll(',', '%2C');
}

export function annotation(diagnostic: Diagnostic): string {
	const { start, end } = diagnostic;
	const endLine = end.line > start.line && end.column === 1 ? end.line - 1 : end.line;
	let range = `line=${start.line},endLine=${endLine}`;
	if (start.line === end.line) range += `,col=${start.column},endColumn=${Math.max(start.column, end.column - 1)}`;
	const level = diagnostic.severity === 'warning'
		? 'warning'
		: diagnostic.severity === 'info' || diagnostic.severity === 'style'
		? 'notice'
		: 'error';
	return `::${level} file=${property(diagnostic.path)},${range},title=${
		property(diagnostic.code || diagnostic.rule)
	}::${commandEscape(diagnostic.message)}`;
}

export function html(value: string): string {
	return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
}

export function summary(result: ActionResult): string {
	let text = `### actionlint: ${result.status}\n\n`;
	const findings = result.diagnostics.length === 1 ? 'finding' : 'findings';
	const files = result.file_count === 1 ? 'workflow file' : 'workflow files';
	text += result.completed
		? `${result.diagnostics.length} ${findings} in ${result.file_count ?? 'an unknown number of'} ${files}.\n\n`
		: `Analysis did not complete (exit ${result.exit_code}).\n\n`;
	if (result.error) text += `<pre>${html(result.error.slice(0, 2000))}</pre>\n\n`;
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		text += `<details><summary>${html(diagnostic.path)}:${diagnostic.start.line}:${diagnostic.start.column} (${
			html(diagnostic.code || diagnostic.rule)
		})</summary>\n\n`;
		text += `<pre>${html(diagnostic.message.slice(0, 1000))}</pre>\n\n`;
		if (diagnostic.snippet) text += `<pre>${html(diagnostic.snippet.slice(0, 2000))}</pre>\n\n`;
		text += '</details>\n\n';
	}
	if (result.diagnostics.length > 50) {
		text += 'Showing the first 50 findings; the persisted result contains all findings.\n\n';
	}
	for (const hint of result.hints) text += `<pre>${html(hint.slice(0, 1000))}</pre>\n\n`;
	return text;
}

export type ReporterRuntime = { log: (text: string) => void; review: typeof postReview };

export async function report(
	result: ActionResult,
	path: string,
	environment: Environment,
	options: ReportOptions,
	runtime: ReporterRuntime,
): Promise<void> {
	const values: Record<string, string> = { 'analysis-result': path, 'report-json': '', 'report-sarif': '' };
	if (options.json) values['report-json'] = path;
	if (options.sarif && result.sarif) {
		const sarifPath = `${path}.sarif`;
		await writeFile(sarifPath, `${JSON.stringify(result.sarif)}\n`, { mode: 0o600 });
		values['report-sarif'] = sarifPath;
	}
	await writeOutputs(environment.GITHUB_OUTPUT, values);
	if (options.annotations && environment.INPUT_FORMAT && environment.INPUT_FORMAT !== 'github') {
		for (const diagnostic of result.diagnostics) runtime.log(annotation(diagnostic));
	}
	if (options.summary && environment.GITHUB_STEP_SUMMARY) {
		await appendFile(environment.GITHUB_STEP_SUMMARY, summary(result));
	}
	if (options.review) {
		try {
			await runtime.review(result, environment);
		} catch (error) {
			runtime.log(
				`::warning::${
					commandEscape(
						`PR review skipped: ${
							error instanceof Error ? error.message : String(error)
						}. Analysis results remain available.`,
					)
				}`,
			);
		}
	}
}

// Result files intentionally survive the action so later steps can upload them.
// Tool-download scratch space has a separate lifetime managed by temporary().
export async function withReporting(
	environment: Environment,
	execute: (environment: Environment) => Promise<number>,
	runtime: ReporterRuntime = { log: console.log, review: postReview },
): Promise<number> {
	const directory = await mkdtemp(join(environment.RUNNER_TEMP || tmpdir(), 'actionlint-result-'));
	const path = join(directory, 'result.json');
	let result = failedResult('The native analysis did not write a completed result');
	await writeFile(path, `${JSON.stringify(result)}\n`, { mode: 0o600 });
	let code = 3;
	let failure: unknown;
	let options: ReportOptions = { annotations: false, summary: false, json: false, sarif: false, review: false };
	try {
		options = reportOptions(environment);
		code = await execute({ ...environment, ACTIONLINT_ACTION_RESULT: path });
		result = parseResult(JSON.parse(await readFile(path, 'utf8')));
		if (!result.completed && code < 2) throw new Error(result.error || 'Native analysis did not complete');
		const acceptedFindings = code === 0 && result.exit_code === 1 && environment['INPUT_FAIL-ON-ERROR'] === 'false';
		if (code !== result.exit_code && !acceptedFindings) {
			throw new Error('Native process status disagrees with its persisted analysis result');
		}
	} catch (error) {
		failure = error;
		result = failedResult(error, error instanceof InputError);
		code = result.exit_code;
		await writeFile(path, `${JSON.stringify(result)}\n`, { mode: 0o600 });
	}
	await report(result, path, environment, options, runtime);
	if (failure !== undefined) throw failure;
	return code;
}
