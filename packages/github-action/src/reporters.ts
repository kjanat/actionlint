import { appendFile, mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, relative, resolve, sep } from 'node:path';

import type { ActionResult, Diagnostic } from '#result';
import { failedResult, parseResult } from '#result';
import { postReview } from '#review';
import type { Environment } from '#runtime';
import { InputError } from '#runtime';
import { commandEscape, writeOutputs } from '#workflow';

export type ReportOptions = {
	annotations: 'auto' | 'true' | 'false';
	summary: boolean;
	sarif: boolean;
	review: boolean;
};

export function reportOptions(environment: Environment): ReportOptions {
	const enabled = (name: string, fallback = 'false'): boolean => {
		const value = environment[`INPUT_${name.toUpperCase()}`] || fallback;
		if (value !== 'true' && value !== 'false') throw new InputError(`Input '${name}' must be 'true' or 'false'`);
		return value === 'true';
	};
	const annotations = environment.INPUT_ANNOTATIONS || 'auto';
	if (annotations !== 'auto' && annotations !== 'true' && annotations !== 'false') {
		throw new InputError("Input 'annotations' must be 'auto', 'true' or 'false'");
	}
	return {
		annotations,
		summary: enabled('summary', 'true'),
		review: enabled('review'),
		sarif: enabled('sarif'),
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
	const findings = result.diagnostics.length === 1 ? 'finding' : 'findings';
	const files = result.file_count === 1 ? 'workflow' : 'workflows';
	const count = `${result.file_count ?? 'an unknown number of'} ${files}`;
	const heading = !result.completed
		? 'Analysis incomplete'
		: result.diagnostics.length > 0
		? `${result.diagnostics.length} ${findings} in ${count}`
		: result.file_count === 0
		? 'No workflows selected'
		: `No findings in ${count}`;
	let text = `### actionlint: ${heading}\n\n`;
	if (!result.completed) text += `Exit ${result.exit_code}; ${result.diagnostics.length} ${findings} collected.\n\n`;
	if (result.error) text += `<pre>${html(result.error.slice(0, 2000))}</pre>\n\n`;
	for (const diagnostic of result.diagnostics.slice(0, 50)) {
		text += `<details><summary>${html(diagnostic.path)}:${diagnostic.start.line}:${diagnostic.start.column} (${
			html(diagnostic.code || diagnostic.rule)
		})</summary>\n\n`;
		text += `<p>${html(diagnostic.message.slice(0, 1000)).replaceAll('\n', '<br>')}</p>\n\n`;
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
	const attempt = async (destination: string, run: () => Promise<void>): Promise<void> => {
		try {
			await run();
		} catch (error) {
			runtime.log(
				`::warning::${
					commandEscape(
						`${destination}: ${
							error instanceof Error ? error.message : String(error)
						}. Analysis results remain available at ${path}.`,
					)
				}`,
			);
		}
	};
	const values: Record<string, string> = { 'result-file': path, 'report-sarif': '' };
	if (options.sarif && result.completed && result.sarif) {
		await attempt('SARIF report unavailable', async () => {
			const sarifPath = `${path}.sarif`;
			await writeFile(sarifPath, `${JSON.stringify(result.sarif)}\n`, { mode: 0o600 });
			values['report-sarif'] = sarifPath;
		});
	}
	await attempt('Report outputs unavailable', () => writeOutputs(environment.GITHUB_OUTPUT, values));
	if (options.annotations === 'true' && environment.INPUT_FORMAT && environment.INPUT_FORMAT !== 'github') {
		const workspace = resolve(environment.GITHUB_WORKSPACE || '.');
		const workingDirectory = resolve(workspace, environment['INPUT_WORKING-DIRECTORY'] || '.');
		for (const diagnostic of result.diagnostics) {
			const path = relative(workspace, resolve(workingDirectory, diagnostic.path)).split(sep).join('/');
			runtime.log(annotation({ ...diagnostic, path }));
		}
	}
	const summaryPath = environment.GITHUB_STEP_SUMMARY;
	if (options.summary && summaryPath) {
		await attempt('Job summary unavailable', () => appendFile(summaryPath, summary(result)));
	}
	if (options.review) {
		await attempt('PR review skipped', async () => {
			runtime.log(commandEscape(await runtime.review(result, environment)));
		});
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
	let options: ReportOptions = {
		annotations: 'auto',
		summary: environment.INPUT_SUMMARY !== 'false',
		sarif: false,
		review: false,
	};
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
		// A process can fail after writing diagnostics. Recover them before recording
		// the failure, and never turn a process/result disagreement into success.
		try {
			result = parseResult(JSON.parse(await readFile(path, 'utf8')));
		} catch {
			// The initialized failure result remains the fallback for missing/corrupt data.
		}
		result = {
			...result,
			...failedResult(error, error instanceof InputError),
			file_count: result.file_count,
			diagnostics: result.diagnostics,
			configurations: result.configurations,
			hints: result.hints,
		};
		code = result.exit_code;
		await writeFile(path, `${JSON.stringify(result)}\n`, { mode: 0o600 });
	}
	await report(result, path, environment, options, runtime);
	if (failure !== undefined) throw failure;
	return code;
}
