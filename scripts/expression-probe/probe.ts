/** biome-ignore-all lint/style/useTemplate: github actions syntax */
/** biome-ignore-all lint/suspicious/noTemplateCurlyInString: github actions syntax */
import { spawnSync } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { isDeepStrictEqual, parseArgs } from 'node:util';
import type { ProbeCase } from './cases.ts';
import { checksFor, corpus } from './cases.ts';

const workflowPath = '.github/workflows/expr-conformance-probe.yml';
const apiVersion = '2026-03-10';
const pause = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
type ObjectValue = Record<string, unknown>;
export function object(value: unknown): ObjectValue {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('Expected an object');
	return Object.fromEntries(Object.entries(value));
}
function jsonObject(text: string): ObjectValue {
	const value: unknown = JSON.parse(text);
	return object(value);
}
function string(value: unknown): string {
	if (typeof value !== 'string') throw new Error('Expected a string');
	return value;
}
function integer(value: unknown): number {
	if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error('Expected a safe integer');
	return value;
}
function command(
	binary: string,
	args: string[],
	input?: string,
): { status: number | null; stdout: string; stderr: string } {
	const result = spawnSync(binary, args, {
		input,
		encoding: 'utf8',
		timeout: 60_000,
		maxBuffer: 20 * 1024 * 1024,
		windowsHide: true,
	});
	if (result.error) throw result.error;
	return { status: result.status, stdout: result.stdout, stderr: result.stderr };
}
type Http = { status: number; body: string; headers: Record<string, string>; stderr: string };
export function parseHttp(stdout: string, stderr = ''): Http {
	const separator = /\r?\n\r?\n/.exec(stdout);
	if (separator === null) throw new Error(`Missing HTTP response: ${stderr || stdout}`);
	const head = stdout.slice(0, separator.index);
	const status = /^HTTP\/\S+ (\d{3})/m.exec(head)?.[1];
	if (status === undefined) throw new Error('Missing HTTP status');
	const headers: Record<string, string> = {};
	const diagnosticHeaders = new Set([
		'content-type',
		'date',
		'x-github-api-version-selected',
		'x-github-request-id',
		'retry-after',
	]);
	for (const line of head.split(/\r?\n/).slice(1)) {
		const colon = line.indexOf(':');
		if (colon <= 0) continue;
		const name = line.slice(0, colon).toLowerCase();
		if (diagnosticHeaders.has(name) || name.startsWith('x-ratelimit-')) {
			headers[name] = line.slice(colon + 1).trim();
		}
	}
	return { status: Number(status), headers, body: stdout.slice(separator.index + separator[0].length), stderr };
}
let nextMutation = 0;
let rateLimitedUntil = 0;
class RateLimitError extends Error {}

/** Retry evidence reads only; dispatches and other mutations are never retried. */
export async function readWithRetry<T>(read: () => Promise<T>, wait = pause): Promise<T> {
	for (let attempt = 0;; attempt++) {
		try {
			return await read();
		} catch (error) {
			if (error instanceof RateLimitError || attempt === 2) throw error;
			await wait(2000 * (attempt + 1));
		}
	}
}

async function api(method: string, path: string, body?: unknown): Promise<Http> {
	if (Date.now() < rateLimitedUntil) {
		throw new RateLimitError('GitHub rate limit cooldown; collect/cleanup after it expires');
	}
	// Serial mutations stay below the general 80/minute secondary limit. A full
	// corpus uses fewer than 500 mutations, including exact-ref cleanup.
	if (method !== 'GET') {
		await pause(Math.max(0, nextMutation - Date.now()));
		nextMutation = Date.now() + 1100;
	}
	const args = ['api', '--include', '--method', method, '-H', `X-GitHub-Api-Version: ${apiVersion}`, path];
	if (body !== undefined) args.push('--input', '-');
	const response = command('gh', args, body === undefined ? undefined : JSON.stringify(body));
	const http = parseHttp(response.stdout, response.stderr);
	if (http.status === 429 || (http.status === 403 && /rate limit/i.test(http.body))) {
		const retry = Number(http.headers['retry-after']);
		const reset = Number(http.headers['x-ratelimit-reset']) * 1000;
		rateLimitedUntil = Math.max(
			Date.now() + Math.max(60_000, Number.isFinite(retry) ? retry * 1000 : 0),
			http.headers['x-ratelimit-remaining'] === '0' && Number.isFinite(reset) ? reset : 0,
		);
		nextMutation = rateLimitedUntil;
		throw new RateLimitError(`GitHub rate limit; stop and resume collection/cleanup later. ${http.body}`);
	}
	return http;
}
function success(http: Http): ObjectValue {
	if (http.status < 200 || http.status >= 300) throw new Error(`HTTP ${http.status}: ${http.body}`);
	return jsonObject(http.body);
}
async function save(path: string, value: unknown): Promise<void> {
	await writeFile(path, `${JSON.stringify(value, null, 2)}\n`);
}
function hash(text: string): string {
	return createHash('sha256').update(text).digest('hex');
}

export function makeWorkflow(probe: ProbeCase, nonce: string): string {
	const title = `expr-probe/${nonce}/${probe.id}`;
	const runName = probe.mode === 'dual' ? `${title}~${'${{'} toJSON(${probe.check.expression}) }}~end` : title;
	const inputs = probe.mode === 'runner' && probe.input !== null
		? `\n    inputs:\n      value:\n        description: Controlled reproduction input\n        required: true\n        type: ${probe.input.type}`
		: '';
	const checks = checksFor(probe);
	const env = checks.map((check, index) =>
		`          PROBE_${index}: ${JSON.stringify('${{ toJSON(' + check.expression + ') }}')}`
	).join('\n');
	const names = JSON.stringify(checks.map(check => check.name));
	const rawInput = probe.mode === 'runner' && probe.input !== null ? '${{ inputs.value }}' : '';
	return `name: Expression conformance probe
run-name: ${JSON.stringify(runName)}
on:
  workflow_dispatch:${inputs}
permissions: {}
jobs:
  measure:
    runs-on: ubuntu-latest
    timeout-minutes: 3
    steps:
      - name: Record environment and strict JSON comparison
        env:
          RAW_INPUT: ${JSON.stringify(rawInput)}
        shell: bash
        run: |
          node <<'NODE'
          const raw = process.env.RAW_INPUT;
          let strictJson = { kind: 'not-measured' };
          if (${probe.mode === 'runner' && probe.input !== null}) {
            try {
              strictJson = { kind: 'parsed', inspected: require('node:util').inspect(JSON.parse(raw), { depth: null, colors: false }) };
            } catch (error) {
              strictJson = { kind: 'rejected', message: error.message };
            }
          }
          console.log('EXPR_PROBE_METADATA ' + JSON.stringify({ nonce: ${JSON.stringify(nonce)}, case: ${
		JSON.stringify(probe.id)
	}, node: process.version, os: process.env.RUNNER_OS, arch: process.env.RUNNER_ARCH, image: process.env.ImageOS, imageVersion: process.env.ImageVersion, raw, strictJson }));
          NODE
      - name: Evaluate expressions
        env:
${env}
        shell: bash
        run: |
          node <<'NODE'
          const names = ${names};
          const values = Object.fromEntries(names.map((name, i) => [name, process.env['PROBE_' + i] ?? null]));
          console.log('EXPR_PROBE_RECORD ' + JSON.stringify({ nonce: ${JSON.stringify(nonce)}, case: ${
		JSON.stringify(probe.id)
	}, values }));
          NODE
`;
}

type Rejection = 'expression-parser' | 'input-validation' | 'service-evaluation' | 'workflow-validation';
export function rejection(http: Http): Rejection | null {
	if (http.status !== 422) return null;
	if (
		/Unrecognized named-value|Unexpected symbol|Unexpected character|Unexpected end|Unclosed expression/i.test(
			http.body,
		)
	) return 'expression-parser';
	if (/fromJson|fromJSON|Error parsing.*Json|Error reading.*Json/.test(http.body)) return 'service-evaluation';
	if (
		/input.*(?:number|valid|type)|(?:number|valid|type).*input/i.test(http.body)
		&& !/failed to parse workflow/i.test(http.body)
	) return 'input-validation';
	if (/failed to parse workflow/i.test(http.body)) return 'workflow-validation';
	return null;
}

type Run = {
	id: number;
	attempt: number;
	url: string;
	sha: string;
	branch: string;
	path: string;
	workflowId: number;
	repositoryId: number;
	event: string;
	title: string;
	status: string;
	conclusion: string | null;
	createdAt: string;
	updatedAt: string;
};
function parseRun(data: ObjectValue): Run {
	return {
		id: integer(data.id),
		attempt: integer(data.run_attempt),
		url: string(data.html_url),
		sha: string(data.head_sha),
		branch: string(data.head_branch),
		path: string(data.path),
		workflowId: integer(data.workflow_id),
		repositoryId: integer(object(data.repository).id),
		event: string(data.event),
		title: string(data.display_title),
		status: string(data.status),
		conclusion: data.conclusion === null ? null : string(data.conclusion),
		createdAt: string(data.created_at),
		updatedAt: string(data.updated_at),
	};
}
type ServiceResult = { kind: 'not-measured' } | { kind: 'value'; serialized: string } | {
	kind: 'missing';
	title: string;
};
export function serviceResult(probe: ProbeCase, nonce: string, title: string): ServiceResult {
	if (probe.mode !== 'dual') return { kind: 'not-measured' };
	const prefix = `expr-probe/${nonce}/${probe.id}~`;
	if (!title.startsWith(prefix) || !title.endsWith('~end')) return { kind: 'missing', title };
	return { kind: 'value', serialized: title.slice(prefix.length, -4) };
}
export function logRecord(log: string, marker: string, nonce: string, caseId: string): ObjectValue | null {
	const matches: ObjectValue[] = [];
	for (const line of log.split(/\r?\n/)) {
		const start = line.indexOf(`${marker} {`);
		if (start < 0) continue;
		try {
			const record = jsonObject(line.slice(start + marker.length + 1));
			if (record.nonce === nonce && record.case === caseId) matches.push(record);
		} catch {
			// Runner logs also echo script source, which is not a measurement.
		}
	}
	if (matches.length > 1) throw new Error(`Ambiguous ${marker} records for ${caseId}`);
	return matches[0] ?? null;
}
type RunnerResult =
	| { kind: 'values'; values: Record<string, string> }
	| { kind: 'evaluation-failed'; message: string }
	| { kind: 'missing'; message: string };
export function runnerResult(probe: ProbeCase, nonce: string, log: string, conclusion: string | null): RunnerResult {
	const record = logRecord(log, 'EXPR_PROBE_RECORD', nonce, probe.id);
	if (record !== null) {
		const values = object(record.values);
		const result: Record<string, string> = {};
		for (const check of checksFor(probe)) result[check.name] = string(values[check.name]);
		if (Object.keys(values).length !== checksFor(probe).length) throw new Error('Unexpected observation count');
		return { kind: 'values', values: result };
	}
	const errors = log.split(/\r?\n/).filter(line => /##\[error\]/.test(line));
	if (
		conclusion === 'failure'
		&& errors.some(line => /fromJSON|fromJson|template is not valid|Error parsing.*Json/i.test(line))
	) {
		return { kind: 'evaluation-failed', message: errors.join('\n') };
	}
	return { kind: 'missing', message: `No measurement record; run conclusion ${conclusion}. ${errors.join('\n')}` };
}

type Outcome =
	| { kind: 'rejected'; phase: Rejection; message: string }
	| { kind: 'pending'; runId: number }
	| {
		kind: 'completed';
		run: Run;
		service: ServiceResult;
		runner: RunnerResult;
		environment: ObjectValue | null;
		runnerVersion: string | null;
	}
	| { kind: 'error'; message: string };
type Entry = { probe: ProbeCase; ref: string; sha: string; workflowSha256: string; dispatch: Http; outcome: Outcome };
type Ref = { ref: string; sha: string; deleted: boolean };
type Session = {
	version: 1;
	repository: string;
	repositoryId: number;
	workflowId: number;
	workflowPath: string;
	nonce: string;
	startedAt: string;
	finishedAt: string | null;
	apiVersion: string;
	source: { revision: string; corpusSha256: string; driverSha256: string };
	requestedCases: number;
	refs: Ref[];
	results: Entry[];
	errors: string[];
};

function dispatchOutcome(dispatch: Http): Outcome {
	const phase = rejection(dispatch);
	if (phase !== null) return { kind: 'rejected', phase, message: string(jsonObject(dispatch.body).message) };
	if (dispatch.status === 200) {
		const runId = integer(jsonObject(dispatch.body).workflow_run_id);
		if (runId <= 0) throw new Error('Invalid dispatched run ID');
		return { kind: 'pending', runId };
	}
	return {
		kind: 'error',
		message: `Unexpected dispatch HTTP ${dispatch.status}: ${dispatch.body}. No automatic redispatch.`,
	};
}

function array(value: unknown): unknown[] {
	if (!Array.isArray(value)) throw new Error('Expected an array');
	return Array.from(value);
}

function digest(value: unknown, length: number): string {
	const text = string(value);
	if (text.length !== length || !/^[a-f0-9]+$/.test(text)) throw new Error('Invalid saved hash');
	return text;
}

/** Rebuild outcomes from retained dispatches, never from untrusted saved verdicts. */
export function sessionForCollection(value: unknown, currentCorpusSha256: string): Session {
	const saved = object(value);
	const source = object(saved.source);
	if (source.corpusSha256 !== currentCorpusSha256) {
		throw new Error('Corpus changed; use the original corpus to collect');
	}
	if (saved.version !== 1 || saved.workflowPath !== workflowPath || saved.apiVersion !== apiVersion) {
		throw new Error('Unsupported capture format, workflow, or API version');
	}
	const repository = string(saved.repository);
	if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid saved repository');
	const nonce = string(saved.nonce);
	if (!/^[a-zA-Z0-9-]+$/.test(nonce)) throw new Error('Invalid saved session nonce');
	const refs = array(saved.refs).map(value => {
		const ref = object(value);
		const name = string(ref.ref);
		const caseId = name.split('/').at(-1);
		if (
			!corpus.some(probe => probe.id === caseId) || name !== `refs/heads/probe/expression-reference/${nonce}/${caseId}`
		) {
			throw new Error('Saved ref does not belong to this session');
		}
		if (typeof ref.deleted !== 'boolean') throw new Error('Invalid saved ref status');
		return { ref: name, sha: digest(ref.sha, 40), deleted: ref.deleted };
	});
	if (new Set(refs.map(ref => ref.ref)).size !== refs.length) throw new Error('Duplicate saved ref');
	const results = array(saved.results).map(value => {
		const entry = object(value);
		const savedProbe = object(entry.probe);
		const probe = corpus.find(probe => probe.id === savedProbe.id);
		if (probe === undefined || !isDeepStrictEqual(probe, savedProbe)) {
			throw new Error('Saved case differs from the corpus');
		}
		const ref = string(entry.ref);
		const sha = digest(entry.sha, 40);
		if (
			ref !== `refs/heads/probe/expression-reference/${nonce}/${probe.id}`
			|| !refs.some(item => item.ref === ref && item.sha === sha)
		) {
			throw new Error('Saved case/ref identity mismatch');
		}
		const response = object(entry.dispatch);
		const dispatch: Http = {
			status: integer(response.status),
			body: string(response.body),
			headers: Object.fromEntries(Object.entries(object(response.headers)).map(([key, value]) => [key, string(value)])),
			stderr: string(response.stderr),
		};
		if (dispatch.status < 100 || dispatch.status > 599) throw new Error('Invalid saved HTTP status');
		return {
			probe,
			ref,
			sha,
			workflowSha256: digest(entry.workflowSha256, 64),
			dispatch,
			outcome: dispatchOutcome(dispatch),
		};
	});
	if (new Set(results.map(entry => entry.probe.id)).size !== results.length) throw new Error('Duplicate saved case');
	const runIds = results.flatMap(entry => entry.outcome.kind === 'pending' ? [entry.outcome.runId] : []);
	if (new Set(runIds).size !== runIds.length) throw new Error('Duplicate dispatched run ID');
	const requestedCases = integer(saved.requestedCases);
	if (requestedCases < results.length || requestedCases > corpus.length) {
		throw new Error('Invalid requested case count');
	}
	return {
		version: 1,
		repository,
		repositoryId: integer(saved.repositoryId),
		workflowId: integer(saved.workflowId),
		workflowPath,
		nonce,
		startedAt: string(saved.startedAt),
		finishedAt: saved.finishedAt === null ? null : string(saved.finishedAt),
		apiVersion,
		source: {
			revision: digest(source.revision, 40),
			corpusSha256: digest(source.corpusSha256, 64),
			driverSha256: digest(source.driverSha256, 64),
		},
		requestedCases,
		refs,
		results,
		errors: array(saved.errors).map(string),
	};
}

async function cleanup(repository: string, repositoryId: number, refs: Ref[], output: string): Promise<void> {
	const currentRepo = success(await api('GET', `repos/${repository}`));
	if (currentRepo.id !== repositoryId) throw new Error('Repository identity changed; refusing cleanup');
	for (const ref of refs) {
		if (ref.deleted) continue;
		if (!/^refs\/heads\/probe\/expression-reference\/[a-zA-Z0-9-]+\/[a-zA-Z0-9-]+$/.test(ref.ref)) {
			throw new Error(`Ref outside the probe namespace: ${ref.ref}`);
		}
		const endpoint = `repos/${repository}/git/${ref.ref}`;
		const current = await api('GET', endpoint.replace('/git/refs/', '/git/ref/'));
		if (current.status !== 404) {
			const data = success(current);
			if (object(data.object).sha !== ref.sha) throw new Error(`Ref changed; refusing deletion: ${ref.ref}`);
			const removed = await api('DELETE', endpoint);
			if (removed.status !== 204) throw new Error(`Cleanup HTTP ${removed.status}: ${removed.body}`);
		}
		ref.deleted = true;
		await save(join(output, 'refs.json'), { repository, repositoryId, refs });
	}
}

async function collect(session: Session, entry: Entry, output: string): Promise<boolean> {
	if (entry.outcome.kind !== 'pending') return true;
	const runId = entry.outcome.runId;
	const runData = await readWithRetry(async () =>
		success(await api('GET', `repos/${session.repository}/actions/runs/${runId}`))
	);
	const run = parseRun(runData);
	if (
		run.repositoryId !== session.repositoryId || run.workflowId !== session.workflowId || run.sha !== entry.sha
		|| run.branch !== entry.ref.slice('refs/heads/'.length) || run.event !== 'workflow_dispatch'
		|| run.path.split('@')[0] !== workflowPath
	) {
		throw new Error(`Run identity mismatch for ${entry.probe.id}`);
	}
	if (run.status !== 'completed') return false;
	const dir = join(output, entry.probe.id);
	await save(join(dir, 'run.json'), runData);
	let logAttempt = 0;
	const logs = await readWithRetry(async () => {
		logAttempt++;
		const result = command('gh', [
			'run',
			'view',
			String(run.id),
			'--repo',
			session.repository,
			'--attempt',
			String(run.attempt),
			'--log',
		]);
		if (result.status !== 0) {
			await writeFile(join(dir, `logs-read-${logAttempt}.stderr.txt`), result.stderr);
			if (/rate limit|HTTP 429/i.test(result.stderr)) {
				rateLimitedUntil = Date.now() + 60_000;
				throw new RateLimitError(`GitHub log read rate limit: ${result.stderr}`);
			}
			throw new Error(`Run ${run.id} exists but logs unavailable: ${result.stderr}`);
		}
		return result;
	});
	await writeFile(join(dir, 'logs.txt'), logs.stdout);
	await writeFile(join(dir, 'logs.stderr.txt'), logs.stderr);
	entry.outcome = {
		kind: 'completed',
		run,
		service: serviceResult(entry.probe, session.nonce, run.title),
		runner: runnerResult(entry.probe, session.nonce, logs.stdout, run.conclusion),
		environment: logRecord(logs.stdout, 'EXPR_PROBE_METADATA', session.nonce, entry.probe.id),
		runnerVersion: /Current runner version: '?([\d.]+)/.exec(logs.stdout)?.[1] ?? null,
	};
	return true;
}

/** Drain every dispatched case even when a different case cannot be collected. */
export async function collectPending(
	session: Session,
	output: string,
	checkpoint: () => Promise<void>,
	interrupted: () => boolean,
	readEntry = collect,
	wait = pause,
): Promise<void> {
	const deadline = Date.now() + 20 * 60_000;
	while (session.results.some(entry => entry.outcome.kind === 'pending')) {
		if (interrupted()) throw new Error('Interrupted during collection; run IDs preserved');
		for (const entry of session.results) {
			if (entry.outcome.kind !== 'pending') continue;
			try {
				await readEntry(session, entry, output);
			} catch (error) {
				if (error instanceof RateLimitError) throw error;
				const message = `${entry.probe.id}: ${error instanceof Error ? error.message : String(error)}`;
				entry.outcome = { kind: 'error', message };
				session.errors.push(message);
			}
			await checkpoint();
		}
		const pending = session.results.filter(entry => entry.outcome.kind === 'pending').length;
		console.log(`${pending} runs pending`);
		if (pending === 0) return;
		if (Date.now() >= deadline) throw new Error('Timed out waiting for runs; run IDs preserved');
		await wait(15_000);
	}
}

function cell(value: string): string {
	const escapes: Record<string, string> = {
		'&': '&amp;',
		'<': '&lt;',
		'>': '&gt;',
		'|': '&#124;',
		'`': '&#96;',
		'\\': '&#92;',
		'*': '&#42;',
		'_': '&#95;',
		'[': '&#91;',
		']': '&#93;',
		'~': '&#126;',
		'\n': '<br>',
	};
	return value.replace(/\r\n?/g, '\n').replace(/[&<>|`\\*_[\]~\n]/g, character => escapes[character] ?? character);
}
export function report(session: Session): string {
	const code = (value: string) => `<code>${cell(value)}</code>`;
	const displayedErrors = new Set<string>();
	const errorText = (message: string) => {
		displayedErrors.add(message);
		return code(message);
	};
	let checkCount = 0;
	const sections = session.results.map(entry => {
		const outcome = entry.outcome;
		const details: string[] = [];
		if (entry.probe.mode === 'runner' && entry.probe.input !== null) {
			details.push(`Supplied input: ${entry.probe.input.type}: ${code(JSON.stringify(entry.probe.input.value))}.`);
		}
		let service = '—';
		let strictJson = 'Not measured';
		if (outcome.kind === 'rejected') {
			details.push(`Dispatch error (${cell(outcome.phase)}): ${errorText(outcome.message)}`);
		}
		if (outcome.kind === 'pending') {
			details.push(`Run: ${outcome.runId}. Status: pending.`);
		}
		if (outcome.kind === 'error') {
			details.push(`Collection error: ${errorText(outcome.message)}`);
			strictJson = 'Unavailable';
		}
		if (outcome.kind === 'completed') {
			details.push(`Run: ${outcome.run.id}, attempt ${outcome.run.attempt}.`);
			if (outcome.service.kind === 'value') service = code(outcome.service.serialized);
			if (outcome.service.kind === 'missing') {
				details.push(`Service result unavailable; recorded title: ${code(outcome.service.title)}`);
			}
			if (outcome.runner.kind !== 'values') {
				const label = outcome.runner.kind === 'evaluation-failed'
					? 'Runner evaluation error'
					: 'Runner result unavailable';
				details.push(`${label}: ${errorText(outcome.runner.message)}`);
			}
			const strict = outcome.environment?.strictJson;
			if (typeof strict === 'object' && strict !== null && !Array.isArray(strict)) {
				const result = object(strict);
				if (result.kind === 'parsed') {
					strictJson = typeof result.inspected === 'string'
						? `Parsed: ${code(result.inspected)}`
						: 'Parsed';
				}
				if (result.kind === 'rejected') {
					strictJson = typeof result.message === 'string'
						? `Rejected: ${code(result.message)}`
						: 'Rejected';
				}
			} else strictJson = 'Unavailable';
		}
		details.push(`Strict JSON input parsing: ${strictJson}.`);
		const rows = checksFor(entry.probe).map(check => {
			const value = outcome.kind === 'completed' && outcome.runner.kind === 'values'
				? outcome.runner.values[check.name]
				: undefined;
			return `| ${code(check.expression)} | ${service} | ${value === undefined ? '—' : code(value)} |`;
		});
		checkCount += rows.length;
		return `## ${entry.probe.id.toLowerCase()}\n\n${
			details.join('\n\n')
		}\n\n| Expression | Service | Runner |\n| --- | --- | --- |\n${rows.join('\n')}`;
	});
	return `# GitHub Actions expression measurements\n\nThese measurements show how GitHub interprets expressions and supplied inputs. Each case records the expressions and their observed results together, so you can reproduce them or compare another evaluator.\n\nSupplied inputs show the declared workflow input type and the JSON value sent to the dispatch API. Service values come from the workflow run name; runner values come from a step on the hosted runner. Both preserve the text returned by \`toJSON()\`. Strict JSON input parsing records JavaScript's \`JSON.parse()\` result. A dash marks a result that was not produced; any associated error appears above its table.\n\nStarted: ${session.startedAt}. Finished: ${
		session.finishedAt ?? 'in progress'
	}.\n\nAPI version: ${code(session.apiVersion)}. Repository: ${code(session.repository)}; source: ${
		code(session.source.revision)
	}.\n\n${session.results.length}/${session.requestedCases} cases recorded; ${checkCount} checks listed.\n\n${
		sections.join('\n\n')
	}\n\n${
		session.errors.filter(message => !displayedErrors.has(message)).map(message => `Error: ${code(message)}`).join('\n')
	}\n`;
}
function measured(entry: Entry): boolean {
	if (entry.outcome.kind === 'rejected') return entry.outcome.phase !== 'workflow-validation';
	if (entry.outcome.kind !== 'completed') return false;
	return entry.outcome.environment !== null && entry.outcome.service.kind !== 'missing'
		&& entry.outcome.runner.kind !== 'missing';
}

async function saveSession(session: Session, output: string): Promise<void> {
	await save(join(output, 'results.json'), session);
	await save(join(output, 'refs.json'), {
		repository: session.repository,
		repositoryId: session.repositoryId,
		refs: session.refs,
	});
	await writeFile(join(output, 'summary.md'), report(session));
}

async function collectSaved(output: string): Promise<void> {
	const previous = await readFile(join(output, 'results.json'), 'utf8');
	const sourceDir = dirname(fileURLToPath(import.meta.url));
	const session = sessionForCollection(jsonObject(previous), hash(await readFile(join(sourceDir, 'cases.ts'), 'utf8')));
	for (const entry of session.results) {
		if (hash(await readFile(join(output, entry.probe.id, 'workflow.yml'), 'utf8')) !== entry.workflowSha256) {
			throw new Error(`Saved workflow changed: ${entry.probe.id}`);
		}
	}
	const startedAt = new Date().toISOString();
	const archive = join(output, 'collections', `${startedAt.replace(/[:.]/g, '-')}-${randomUUID().slice(0, 8)}`);
	await mkdir(archive, { recursive: true });
	await writeFile(join(archive, 'results.before.json'), previous);
	for (const entry of session.results) {
		const archivedCase = join(archive, entry.probe.id);
		await mkdir(archivedCase);
		for (const name of ['run.json', 'logs.txt', 'logs.stderr.txt']) {
			try {
				await copyFile(join(output, entry.probe.id, name), join(archivedCase, name));
			} catch (error) {
				if (!(error instanceof Error && 'code' in error && error.code === 'ENOENT')) throw error;
			}
		}
	}
	const oldErrors = session.errors.length;
	let interrupted = false;
	const interrupt = () => {
		interrupted = true;
	};
	process.on('SIGINT', interrupt);
	process.on('SIGTERM', interrupt);
	try {
		await collectPending(session, output, () => saveSession(session, output), () => interrupted);
	} catch (error) {
		session.errors.push(error instanceof Error ? error.message : String(error));
	} finally {
		await saveSession(session, output);
		await save(join(archive, 'collection.json'), {
			startedAt,
			finishedAt: new Date().toISOString(),
			driverSha256: hash(await readFile(fileURLToPath(import.meta.url), 'utf8')),
			errors: session.errors.slice(oldErrors),
		});
		process.off('SIGINT', interrupt);
		process.off('SIGTERM', interrupt);
	}
	const complete = session.results.length === session.requestedCases && session.results.every(measured)
		&& session.errors.length === oldErrors;
	console.log(
		`${complete ? 'Complete' : 'Incomplete'} collected evidence: ${output}. Original capture retained in ${archive}`,
	);
	if (!complete) throw new Error('Collection remains incomplete; inspect summary.md. No cases were dispatched.');
}

async function run(repository: string, output: string, selected: ProbeCase[]): Promise<void> {
	if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Use OWNER/REPO');
	await mkdir(dirname(output), { recursive: true });
	await mkdir(output); // Never overwrite a previous capture.
	const repo = success(await api('GET', `repos/${repository}`));
	const workflow = success(await api('GET', `repos/${repository}/actions/workflows/expr-conformance-probe.yml`));
	if (workflow.path !== workflowPath || workflow.state !== 'active') {
		throw new Error('The probe workflow must already be active on the default branch');
	}
	const revision = command('git', ['rev-parse', 'HEAD']);
	if (revision.status !== 0) throw new Error(revision.stderr);
	const sourceDir = dirname(fileURLToPath(import.meta.url));
	const session: Session = {
		version: 1,
		repository,
		repositoryId: integer(repo.id),
		workflowId: integer(workflow.id),
		workflowPath,
		nonce: `${new Date().toISOString().replace(/[-:.TZ]/g, '')}-${randomUUID().slice(0, 8)}`,
		startedAt: new Date().toISOString(),
		finishedAt: null,
		apiVersion,
		source: {
			revision: revision.stdout.trim(),
			corpusSha256: hash(await readFile(join(sourceDir, 'cases.ts'), 'utf8')),
			driverSha256: hash(await readFile(fileURLToPath(import.meta.url), 'utf8')),
		},
		requestedCases: selected.length,
		refs: [],
		results: [],
		errors: [],
	};
	await save(join(output, 'corpus.json'), selected);
	const checkpoint = () => saveSession(session, output);
	let interrupted = false;
	const interrupt = () => {
		interrupted = true;
	};
	process.on('SIGINT', interrupt);
	process.on('SIGTERM', interrupt);
	try {
		await checkpoint();
		try {
			for (const probe of selected) {
				if (interrupted) throw new Error('Interrupted; remaining runs are recorded in results.json');
				const dir = join(output, probe.id);
				await mkdir(dir);
				const yaml = makeWorkflow(probe, session.nonce);
				await writeFile(join(dir, 'workflow.yml'), yaml);
				const tree = success(
					await api('POST', `repos/${repository}/git/trees`, {
						tree: [{ path: workflowPath, mode: '100644', type: 'blob', content: yaml }],
					}),
				);
				const commit = success(
					await api('POST', `repos/${repository}/git/commits`, {
						message: `test: reproduce expression case ${probe.id}`,
						tree: string(tree.sha),
						parents: [],
					}),
				);
				const sha = string(commit.sha);
				const ref = `refs/heads/probe/expression-reference/${session.nonce}/${probe.id}`;
				session.refs.push({ ref, sha, deleted: false });
				await checkpoint(); // Journal before an uncertain ref creation can occur.
				success(await api('POST', `repos/${repository}/git/refs`, { ref, sha }));
				const request = probe.mode === 'runner' && probe.input !== null
					? { ref: ref.slice('refs/heads/'.length), inputs: { value: probe.input.value } }
					: { ref: ref.slice('refs/heads/'.length) };
				await save(join(dir, 'dispatch-request.json'), request);
				// Never retry a dispatch after an uncertain response: it may have run.
				const dispatch = await api(
					'POST',
					`repos/${repository}/actions/workflows/${session.workflowId}/dispatches`,
					request,
				);
				await save(join(dir, 'dispatch-response.json'), dispatch);
				const outcome = dispatchOutcome(dispatch);
				session.results.push({ probe, ref, sha, workflowSha256: hash(yaml), dispatch, outcome });
				await checkpoint();
				console.log(`${session.results.length}/${selected.length} ${probe.id}: ${outcome.kind}`);
				if (outcome.kind === 'error') throw new Error(outcome.message);
			}
		} catch (error) {
			session.errors.push(error instanceof Error ? error.message : String(error));
		}
		await collectPending(session, output, checkpoint, () => interrupted);
	} catch (error) {
		session.errors.push(error instanceof Error ? error.message : String(error));
	} finally {
		try {
			await cleanup(repository, session.repositoryId, session.refs, output);
		} catch (error) {
			session.errors.push(`Cleanup: ${error instanceof Error ? error.message : String(error)}`);
		}
		session.finishedAt = new Date().toISOString();
		await checkpoint();
		process.off('SIGINT', interrupt);
		process.off('SIGTERM', interrupt);
	}
	const complete = session.results.length === selected.length && session.results.every(measured)
		&& session.errors.length === 0;
	console.log(`${complete ? 'Complete' : 'Incomplete'} evidence: ${output}`);
	if (!complete) {
		throw new Error(session.errors.join('\n') || 'Some cases lack complete measurements; inspect summary.md');
	}
}

async function main(): Promise<void> {
	const { values, positionals } = parseArgs({
		allowPositionals: true,
		options: {
			repo: { type: 'string' },
			output: { type: 'string' },
			case: { type: 'string', multiple: true },
			group: { type: 'string' },
		},
	});
	const verb = positionals[0];
	if (verb === 'list') {
		for (const probe of corpus) console.log(`${probe.id}\t${probe.group}\t${checksFor(probe).length} checks`);
		return;
	}
	if (values.output === undefined) {
		throw new Error(
			'Use run --repo OWNER/REPO --output NEW_DIRECTORY [--group GROUP | --case ID], collect --output DIRECTORY, or cleanup --output DIRECTORY',
		);
	}
	const output = resolve(values.output);
	if (verb === 'collect') {
		await collectSaved(output);
		return;
	}
	if (verb === 'cleanup') {
		const journal = jsonObject(await readFile(join(output, 'refs.json'), 'utf8'));
		if (!Array.isArray(journal.refs)) throw new Error('Invalid refs journal');
		const refs = journal.refs.map(value => {
			const ref = object(value);
			if (typeof ref.deleted !== 'boolean') throw new Error('Invalid cleanup status');
			return { ref: string(ref.ref), sha: string(ref.sha), deleted: ref.deleted };
		});
		await cleanup(string(journal.repository), integer(journal.repositoryId), refs, output);
		return;
	}
	if (verb !== 'run' || values.repo === undefined) throw new Error('Use run --repo OWNER/REPO --output NEW_DIRECTORY');
	for (const id of values.case ?? []) {
		if (!corpus.some(probe => probe.id === id)) throw new Error(`Unknown case: ${id}`);
	}
	const selected = corpus.filter(probe =>
		(values.group === undefined || probe.group === values.group)
		&& (values.case === undefined || values.case.includes(probe.id))
	);
	if (selected.length === 0) throw new Error('No cases selected');
	await run(values.repo, output, selected);
}

if (import.meta.main) {
	main().catch(error => {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	});
}
