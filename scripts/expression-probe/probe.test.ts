import assert from 'node:assert/strict';
import { test } from 'node:test';
import { checksFor, corpus, quoteExpression } from './cases.ts';
import {
	collectPending,
	logRecord,
	makeWorkflow,
	parseHttp,
	readWithRetry,
	rejection,
	report,
	runnerResult,
	serviceResult,
	sessionForCollection,
} from './probe.ts';

function find(id: string) {
	const probe = corpus.find(probe => probe.id === id);
	if (probe === undefined) throw new Error(`Missing fixture ${id}`);
	return probe;
}

test('covers the 44 literal cases and 34 parser payloads', () => {
	assert.equal(corpus.length, 85);
	assert.equal(corpus.filter(probe => probe.group === 'literals').length, 44);
	assert.equal(corpus.filter(probe => probe.group === 'from-json').length, 34);
	assert.equal(new Set(corpus.map(probe => probe.id)).size, corpus.length);
	for (const probe of corpus) {
		assert.match(probe.id, /^[a-zA-Z0-9-]+$/);
		assert.equal(new Set(checksFor(probe).map(check => check.name)).size, checksFor(probe).length);
	}
	assert.equal(checksFor(find('coercion-strings')).length, 156);
	assert.equal(find('literal-42').mode, 'dual');
});

test('expression quoting preserves apostrophes without changing the expression grammar', () => {
	assert.equal(quoteExpression("it's 017"), "'it''s 017'");
});

test('generated cases contain no checkout, credential references or shell interpolation of measured values', () => {
	for (const probe of corpus) {
		const yaml = makeWorkflow(probe, 'test-session');
		assert.match(yaml, /permissions: \{\}/);
		assert.doesNotMatch(yaml, /secrets\.|github\.token|uses:|pull_request_target/);
		const runs = [...yaml.matchAll(/        run: \|\n([\s\S]*?)(?=      - name:|$)/g)];
		assert.equal(runs.length, 2);
		for (const run of runs) assert.doesNotMatch(run[1] ?? '', /\$\{\{/);
	}
});

test('array and object results use toJSON rather than implicit string conversion', () => {
	const yaml = makeWorkflow(find('json-trailing-comma-array'), 'nonce');
	assert.match(yaml, /PROBE_\d+: "\$\{\{ toJSON\(fromJSON\(inputs\.value\)\) \}\}"/);
	assert.match(yaml, /run-name: "expr-probe\/nonce\/json-trailing-comma-array"/);
	assert.doesNotMatch(yaml, /\[1,\]/); // Payload travels through dispatch inputs, not source interpolation.
});

test('service values require exact case identity and both framing markers', () => {
	const probe = find('literal-14');
	assert.deepEqual(serviceResult(probe, 'nonce', 'expr-probe/nonce/literal-14~Infinity~end'), {
		kind: 'value',
		serialized: 'Infinity',
	});
	assert.equal(serviceResult(probe, 'nonce', 'expr-probe/nonce/literal-14~Infinity').kind, 'missing');
	assert.equal(serviceResult(probe, 'nonce', 'expr-probe/wrong/literal-14~Infinity~end').kind, 'missing');
	assert.equal(serviceResult(find('json-hex'), 'nonce', 'ordinary title').kind, 'not-measured');
});

test('HTTP parsing captures error bodies and does not equate every 422 with parser rejection', () => {
	const parser = parseHttp(
		'HTTP/2.0 422 Unprocessable Entity\r\nX-GitHub-Request-Id: example\r\n\r\n{"message":"failed to parse workflow: Unrecognized named-value: 0O17"}',
	);
	assert.equal(parser.headers['x-github-request-id'], 'example');
	assert.equal(rejection(parser), 'expression-parser');
	assert.equal(rejection(parseHttp('HTTP/2.0 422 Unprocessable Entity\n\n{"message":"No ref found for: gone"}')), null);
	assert.equal(rejection(parseHttp('HTTP/2.0 403 Forbidden\n\n{"message":"Unrecognized named-value"}')), null);
	assert.equal(parseHttp('HTTP/2.0 200 OK\n\n{"workflow_run_id":123}').status, 200);
	assert.throws(() => parseHttp('', 'network failed'), /Missing HTTP response/);
});

test('HTTP evidence retains diagnostic headers and the exact body while excluding credential metadata', () => {
	const body = '  {"message":"Invalid input"}\r\n';
	const response = parseHttp([
		'HTTP/2.0 422 Unprocessable Entity',
		'Content-Type: application/json; charset=utf-8',
		'Date: Mon, 14 Sep 2026 12:00:00 GMT',
		'X-GitHub-Api-Version-Selected: 2026-03-10',
		'X-GitHub-Request-Id: example',
		'Retry-After: 60',
		'X-RateLimit-Remaining: 100',
		'X-RateLimit-Reset: 1789387260',
		'X-OAuth-Scopes: repo, workflow',
		'x-accepted-oauth-scopes: repo',
		'Authorization: Bearer test-only',
		'Set-Cookie: test-only',
		'X-Unrelated-Header: excluded',
		'',
		body,
	].join('\r\n'));
	assert.equal(response.status, 422);
	assert.equal(response.body, body);
	assert.deepEqual(response.headers, {
		'content-type': 'application/json; charset=utf-8',
		date: 'Mon, 14 Sep 2026 12:00:00 GMT',
		'x-github-api-version-selected': '2026-03-10',
		'x-github-request-id': 'example',
		'retry-after': '60',
		'x-ratelimit-remaining': '100',
		'x-ratelimit-reset': '1789387260',
	});
});

test('log parsing ignores echoed source, preserves nonfinite serialization, and requires all observations', () => {
	const probe = find('literal-14');
	const prefix = 'measure\tEvaluate expressions\t2026-09-14T12:00:00Z ';
	const source = "console.log('EXPR_PROBE_RECORD ' + JSON.stringify({ nonce: 'nonce', case: 'literal-14' }));";
	const record = JSON.stringify({ nonce: 'nonce', case: probe.id, values: { value: 'Infinity' } });
	const log = `${prefix}${source}\n${prefix}EXPR_PROBE_RECORD ${record}\n`;
	assert.deepEqual(runnerResult(probe, 'nonce', log, 'success'), { kind: 'values', values: { value: 'Infinity' } });
	assert.equal(logRecord(log, 'EXPR_PROBE_RECORD', 'wrong', probe.id), null);
	assert.throws(() => runnerResult(probe, 'nonce', log + log, 'success'), /Ambiguous/);
	assert.throws(
		() =>
			runnerResult(
				probe,
				'nonce',
				`${prefix}EXPR_PROBE_RECORD ${JSON.stringify({ nonce: 'nonce', case: probe.id, values: {} })}`,
				'success',
			),
		/Expected a string/,
	);
});

test('a failed evaluation is distinct from a failed runner, timeout, or absent log record', () => {
	const probe = find('json-octal-prefix');
	assert.equal(
		runnerResult(probe, 'nonce', '##[error]The template is not valid. Error fromJSON: invalid character', 'failure')
			.kind,
		'evaluation-failed',
	);
	assert.equal(runnerResult(probe, 'nonce', '##[error]Runner lost communication', 'failure').kind, 'missing');
	assert.equal(runnerResult(probe, 'nonce', '', 'success').kind, 'missing');
	assert.equal(runnerResult(probe, 'nonce', '', 'cancelled').kind, 'missing');
});

test('evidence reads retry transient failures, preserve the result, and stop after three attempts', async () => {
	let attempts = 0;
	const delays: number[] = [];
	const wait = async (ms: number) => {
		delays.push(ms);
	};
	const value = await readWithRetry(async () => {
		if (++attempts < 3) throw new Error('Logs not ready');
		return 'measurement';
	}, wait);
	assert.equal(value, 'measurement');
	assert.equal(attempts, 3);
	assert.deepEqual(delays, [2000, 4000]);
	attempts = 0;
	await assert.rejects(
		readWithRetry(async () => {
			attempts++;
			throw new Error('Still unavailable');
		}, wait),
		/Still unavailable/,
	);
	assert.equal(attempts, 3);
});

function captureFixture() {
	const nonce = 'test-session';
	const results = ['literal-01', 'literal-02'].map((id, index) => ({
		probe: find(id),
		ref: `refs/heads/probe/expression-reference/${nonce}/${id}`,
		sha: String(index + 1).repeat(40),
		workflowSha256: 'a'.repeat(64),
		dispatch: parseHttp(`HTTP/2.0 200 OK\n\n${JSON.stringify({ workflow_run_id: 101 + index })}`),
		outcome: { kind: 'error', message: 'Previous capture lacked logs' },
	}));
	return {
		version: 1,
		repository: 'example/repository',
		repositoryId: 123,
		workflowId: 456,
		workflowPath: '.github/workflows/expr-conformance-probe.yml',
		nonce,
		startedAt: '2026-09-14T12:00:00Z',
		finishedAt: '2026-09-14T12:10:00Z',
		apiVersion: '2026-03-10',
		source: { revision: 'b'.repeat(40), corpusSha256: 'c'.repeat(64), driverSha256: 'd'.repeat(64) },
		requestedCases: 2,
		refs: results.map(entry => ({ ref: entry.ref, sha: entry.sha, deleted: true })),
		results,
		errors: ['Previous capture lacked logs'],
	};
}

test('collection rebuilds run IDs from original dispatches without changing source or dispatch records', () => {
	const saved = captureFixture();
	const session = sessionForCollection(saved, 'c'.repeat(64));
	assert.deepEqual(session.results.map(entry => entry.outcome), [
		{ kind: 'pending', runId: 101 },
		{ kind: 'pending', runId: 102 },
	]);
	assert.deepEqual(session.source, saved.source);
	assert.equal(session.startedAt, saved.startedAt);
	assert.equal(session.finishedAt, saved.finishedAt);
	assert.deepEqual(session.results.map(entry => entry.dispatch), saved.results.map(entry => entry.dispatch));
	assert.deepEqual(session.errors, saved.errors);
	assert.deepEqual(saved.results.map(entry => entry.outcome.kind), ['error', 'error']);
});

test('collection rejects changed corpora, changed case definitions, foreign refs, and invalid dispatch IDs', () => {
	assert.throws(() => sessionForCollection(captureFixture(), 'e'.repeat(64)), /Corpus changed/);
	const changedCase = captureFixture();
	const entry = changedCase.results[0];
	assert.ok(entry);
	entry.probe = { ...entry.probe, id: '../foreign' };
	assert.throws(() => sessionForCollection(changedCase, 'c'.repeat(64)), /Saved case differs/);
	const foreignRef = captureFixture();
	const ref = foreignRef.refs[0];
	assert.ok(ref);
	ref.ref = 'refs/heads/main';
	assert.throws(() => sessionForCollection(foreignRef, 'c'.repeat(64)), /does not belong/);
	const badId = captureFixture();
	const dispatch = badId.results[0]?.dispatch;
	assert.ok(dispatch);
	dispatch.body = '{"workflow_run_id":"101"}';
	assert.throws(() => sessionForCollection(badId, 'c'.repeat(64)), /Expected a safe integer/);
});

test('collection continues to later dispatched cases after one evidence read fails', async () => {
	const session = sessionForCollection(captureFixture(), 'c'.repeat(64));
	session.errors.push('A later dispatch failed');
	const visited: string[] = [];
	let checkpoints = 0;
	await collectPending(
		session,
		'unused',
		async () => {
			checkpoints++;
		},
		() => false,
		async (_session, entry) => {
			visited.push(entry.probe.id);
			if (entry.probe.id === 'literal-01') throw new Error('Evidence unavailable after retries');
			entry.outcome = { kind: 'rejected', phase: 'expression-parser', message: 'Test collector completed' };
			return true;
		},
	);
	assert.deepEqual(visited, ['literal-01', 'literal-02']);
	assert.equal(checkpoints, 2);
	assert.deepEqual(session.results.map(entry => entry.outcome.kind), ['error', 'rejected']);
	assert.ok(session.errors.includes('A later dispatch failed'));
	assert.ok(session.errors.some(error => error.includes('Evidence unavailable after retries')));
});

test('reference report groups inputs and errors by stable case anchors without live log links', () => {
	const session = sessionForCollection(captureFixture(), 'c'.repeat(64));
	const literal = session.results[0];
	const payload = session.results[1];
	assert.ok(literal);
	assert.ok(payload);
	literal.outcome = {
		kind: 'completed',
		run: {
			id: 101,
			attempt: 1,
			url: 'https://github.com/example/repository/actions/runs/101',
			sha: literal.sha,
			branch: literal.ref,
			path: session.workflowPath,
			workflowId: session.workflowId,
			repositoryId: session.repositoryId,
			event: 'workflow_dispatch',
			title: 'test',
			status: 'completed',
			conclusion: 'success',
			createdAt: session.startedAt,
			updatedAt: session.startedAt,
		},
		service: { kind: 'value', serialized: '9.00719925474099E+15' },
		runner: { kind: 'values', values: { value: '9007199254740992' } },
		environment: { strictJson: { kind: 'not-measured' } },
		runnerVersion: '2.336.0',
	};
	payload.probe = {
		id: 'input-string-NaN',
		group: 'from-json',
		mode: 'runner',
		input: { type: 'string', value: '<a>|`&\n' },
		checks: [{ name: 'value', expression: 'fromJSON(inputs.value)' }, {
			name: 'n',
			expression: 'fromJSON(inputs.value).n',
		}],
	};
	payload.outcome = {
		...literal.outcome,
		run: { ...literal.outcome.run, id: 102, url: 'https://github.com/example/repository/actions/runs/102' },
		service: { kind: 'not-measured' },
		runner: { kind: 'values', values: { value: '<a>|`&\n*_[x]~', n: 'NaN' } },
		environment: { strictJson: { kind: 'rejected', message: 'Unexpected <value>' } },
	};
	const captured = JSON.stringify(session);
	const markdown = report(session);
	assert.equal(JSON.stringify(session), captured);
	const rows = markdown.split('\n').filter(line => line.startsWith('| <code>'));
	assert.equal(rows.length, 3);
	assert.ok(rows.every(row => (row.match(/\|/g) ?? []).length === 4));
	assert.match(markdown, /^## literal-01$/m);
	assert.match(markdown, /^## input-string-nan$/m);
	assert.equal((markdown.match(/Supplied input:/g) ?? []).length, 1);
	assert.equal((markdown.match(/Strict JSON input parsing:/g) ?? []).length, 2);
	assert.match(markdown, /<code>fromJSON\(inputs\.value\)<\/code>/);
	assert.match(markdown, /<code>fromJSON\(inputs\.value\)\.n<\/code>/);
	assert.match(markdown, /<code>9\.00719925474099E\+15<\/code>/);
	assert.match(markdown, /<code>9007199254740992<\/code>/);
	assert.match(markdown, /<code>NaN<\/code>/);
	assert.ok(markdown.includes('string: <code>"&lt;a&gt;&#124;&#96;&amp;&#92;n"</code>'));
	assert.ok(markdown.includes('<code>&lt;a&gt;&#124;&#96;&amp;<br>&#42;&#95;&#91;x&#93;&#126;</code>'));
	assert.ok(markdown.includes('Rejected: <code>Unexpected &lt;value&gt;</code>'));
	assert.equal((markdown.match(/Unexpected &lt;value&gt;/g) ?? []).length, 1);
	assert.ok(markdown.includes('Run: 101, attempt 1.'));
	assert.ok(markdown.includes('Run: 102, attempt 1.'));
	assert.doesNotMatch(markdown, /https:\/\/github\.com\/|\]\(https?:/);
	assert.equal((markdown.match(/API version:/g) ?? []).length, 1);
	assert.doesNotMatch(markdown, /A parser rejection is a measurement/);

	const evaluationMessage = 'Cannot parse <input>|`';
	payload.outcome.runner = { kind: 'evaluation-failed', message: evaluationMessage };
	session.errors.push(evaluationMessage);
	const failed = report(session);
	assert.equal((failed.match(/Cannot parse &lt;input&gt;&#124;&#96;/g) ?? []).length, 1);
	assert.equal((failed.match(/Runner evaluation error:/g) ?? []).length, 1);
	assert.ok(failed.includes('| <code>fromJSON(inputs.value).n</code> | — | — |'));

	payload.outcome = { kind: 'rejected', phase: 'input-validation', message: 'Invalid supplied number' };
	const rejected = report(session);
	assert.equal((rejected.match(/Invalid supplied number/g) ?? []).length, 1);
	assert.equal((rejected.match(/Dispatch error \(input-validation\):/g) ?? []).length, 1);
});
