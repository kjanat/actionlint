// Exercise the published Node entrypoint and the ordinary actionlint binary with
// the same INPUT_* environment variables supplied by the Actions runner.
import assert from 'node:assert/strict';

import { spawnSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, relative, resolve } from 'node:path';

const [actionDirectory, binary] = process.argv.slice(2);
if (!actionDirectory || !binary) throw new Error('Usage: test-action.mjs ACTION_DIRECTORY ACTIONLINT_BINARY');
const source = process.cwd();
const entrypoint = resolve(actionDirectory, 'action.mjs');
const tempRoot = resolve(tmpdir());
const workspace = await mkdtemp(join(tempRoot, 'actionlint-action-test-'));
const shared = await mkdtemp(join(tempRoot, 'actionlint-shared-test-'));
const outputFile = join(workspace, 'github-output');
const summaryFile = join(workspace, 'github-summary');

/** @param {Record<string, string>} inputs @param {number} expected */
async function run(inputs, expected = 0) {
	await writeFile(outputFile, '');
	await writeFile(summaryFile, '');
	/** @type {NodeJS.ProcessEnv} */
	const env = { ...process.env };
	for (const key of Object.keys(env)) if (key.startsWith('INPUT_')) delete env[key];
	for (
		const [name, value] of Object.entries({
			files: 'testdata/ok/minimal.yaml',
			format: 'json',
			shellcheck: 'true',
			pyflakes: 'true',
			'add-actionlint-to-path': 'false',
			'add-shellcheck-to-path': 'false',
			'add-pyflakes-to-path': 'false',
			'working-directory': '.',
			'fail-on-error': 'true',
			...inputs,
		})
	) env[`INPUT_${name.toUpperCase()}`] = value;
	env.ACTIONLINT_ACTION_BINARY = resolve(binary);
	env.GITHUB_WORKSPACE = workspace;
	env.GITHUB_OUTPUT = outputFile;
	env.GITHUB_STEP_SUMMARY = summaryFile;
	env.RUNNER_TEMP = workspace;
	env.GITHUB_ACTIONS = 'true';
	const result = spawnSync(process.execPath, [entrypoint], { cwd: workspace, env, encoding: 'utf8' });
	if (result.error) throw result.error;
	const log = `${result.stdout}\n${result.stderr}`;
	assert.equal(result.status, expected, log);
	/** @type {Map<string, string>} */
	const outputs = new Map();
	const lines = (await readFile(outputFile, 'utf8')).split(/\r?\n/);
	for (let i = 0; i < lines.length; i++) {
		const match = /^([^<]+)<<(.+)$/.exec(lines[i]);
		if (!match) continue;
		const value = [];
		while (++i < lines.length && lines[i] !== match[2]) value.push(lines[i]);
		outputs.set(match[1], value.join('\n'));
	}
	const resultPath = outputs.get('result-file');
	assert.ok(resultPath, `missing persisted result: ${log}`);
	const analysis = JSON.parse(await readFile(resultPath, 'utf8'));
	assert.equal(analysis.schema_version, 1);
	if (outputs.has('result')) assert.equal(analysis.status, outputs.get('result'), log);
	if (outputs.has('exit-code')) assert.equal(String(analysis.exit_code), outputs.get('exit-code'), log);
	if (expected >= 2) assert.equal(analysis.exit_code, expected, log);
	assert.equal(analysis.completed, analysis.exit_code < 2, log);
	return { outputs, log, analysis };
}

try {
	await mkdir(join(workspace, '.git'));
	await mkdir(join(workspace, '.github/workflows'), { recursive: true });
	for (
		const name of [
			'ok/minimal.yaml',
			'err/one_error.yaml',
			'err/shellcheck_default_shell_detection.yaml',
			'err/pyflakes_step_shell.yaml',
		]
	) {
		const destination = join(workspace, 'testdata', name);
		await mkdir(dirname(destination), { recursive: true });
		await copyFile(join(source, 'testdata', name), destination);
	}
	const clean = await run({});
	for (
		const [name, expected] of Object.entries({
			'exit-code': '0',
			result: 'success',
			'problems-found': 'false',
			'problem-count': '0',
			output: '[]',
		})
	) {
		assert.equal(clean.outputs.get(name), expected, clean.log);
	}
	assert.match(clean.log, /0 problems in 1 workflow file \(requested tools: shellcheck, pyflakes\)/);
	for (const format of ['github', 'default', 'oneline', 'json', 'json-lines', 'markdown', 'sarif']) {
		const result = await run({
			files: 'testdata/err/one_error.yaml',
			format,
			'fail-on-error': 'false',
			annotations: 'true',
			summary: 'true',
			sarif: 'true',
		});
		assert.equal(result.outputs.get('exit-code'), '1', result.log);
		assert.equal(result.outputs.get('result'), 'problems-found', result.log);
		assert.equal(result.outputs.get('problems-found'), 'true', result.log);
		assert.equal(result.outputs.get('problem-count'), '1', result.log);
		assert.equal(result.analysis.diagnostics.length, 1);
		assert.equal(
			(result.log.match(/::error file=/g) || []).length,
			1,
			`duplicate or missing annotation: ${result.log}`,
		);
		assert.match(await readFile(summaryFile, 'utf8'), /1 finding in 1 workflow/);
		assert.ok(result.outputs.get('result-file'));
		assert.equal(result.outputs.has('report-json'), false);
		const sarifPath = result.outputs.get('report-sarif');
		assert.ok(sarifPath);
		assert.equal(JSON.parse(await readFile(sarifPath, 'utf8')).runs[0].results.length, 1);
	}
	const quietAnnotations = await run({
		files: 'testdata/err/one_error.yaml',
		format: 'github',
		annotations: 'false',
		'fail-on-error': 'false',
	});
	assert.doesNotMatch(quietAnnotations.log, /::(?:error|warning|notice) file=/);
	assert.match(quietAnnotations.outputs.get('output') || '', /::error file=/);
	await run({ summary: 'false' });
	assert.equal(await readFile(summaryFile, 'utf8'), '');
	await run({});
	assert.match(await readFile(summaryFile, 'utf8'), /No findings in 1 workflow/);
	await run({ files: '' }, 3);
	const shell = await run({
		files: 'testdata/err/shellcheck_default_shell_detection.yaml',
		pyflakes: 'false',
		'fail-on-error': 'false',
	});
	assert.equal(shell.outputs.get('problem-count'), '12', shell.log);
	await writeFile(
		join(workspace, 'shell-fix.yaml'),
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          value="$1"\n          echo $value\n',
	);
	const fixable = await run({ files: 'shell-fix.yaml', pyflakes: 'false', 'fail-on-error': 'false' });
	const quote = fixable.analysis.diagnostics.find((diagnostic) => diagnostic.code === 'SC2086');
	assert.ok(quote, fixable.log);
	assert.equal(quote.severity, 'info');
	assert.ok(quote.fixes.length > 0);
	assert.ok(quote.fixes[0].edits.every((edit) => edit.path === 'shell-fix.yaml' && edit.start.line === 9));
	const sarifQuote = fixable.analysis.sarif.runs[0].results.find((diagnostic) =>
		diagnostic.properties?.externalCode === 'SC2086'
	);
	assert.ok(sarifQuote);
	assert.equal(sarifQuote.level, 'note');
	assert.equal(fixable.analysis.sarif.runs[0].columnKind, 'unicodeCodePoints');
	assert.equal(sarifQuote.fixes[0].artifactChanges[0].artifactLocation.uri, 'shell-fix.yaml');
	assert.equal(sarifQuote.fixes[0].artifactChanges[0].replacements.length, quote.fixes[0].edits.length);
	const noShell = await run({
		files: 'testdata/err/shellcheck_default_shell_detection.yaml',
		shellcheck: 'false',
		pyflakes: 'false',
	});
	assert.equal(noShell.outputs.get('problem-count'), '0', noShell.log);
	const configuredNoShell = await run({
		files: 'testdata/err/shellcheck_default_shell_detection.yaml',
		config: 'tools: {shellcheck: {enabled: false}}',
		pyflakes: 'false',
	});
	assert.equal(configuredNoShell.outputs.get('problem-count'), '0', configuredNoShell.log);
	const python = await run({
		files: 'testdata/err/pyflakes_step_shell.yaml',
		shellcheck: 'false',
		'fail-on-error': 'false',
	});
	assert.equal(python.outputs.get('problem-count'), '3', python.log);
	const noPython = await run({
		files: 'testdata/err/pyflakes_step_shell.yaml',
		shellcheck: 'false',
		pyflakes: 'false',
	});
	assert.equal(noPython.outputs.get('problem-count'), '0', noPython.log);
	const saved = await run({
		files: 'testdata/err/one_error.yaml',
		format: 'json-lines',
		'output-file': 'actionlint-results.jsonl',
		'fail-on-error': 'false',
	});
	assert.equal(saved.outputs.get('output-file'), 'actionlint-results.jsonl', saved.log);
	assert.match(await readFile(join(workspace, 'actionlint-results.jsonl'), 'utf8'), /"message"/);
	await run({ files: 'testdata/err/one_error.yaml' }, 1);
	await copyFile(join(source, 'testdata/ok/minimal.yaml'), join(shared, 'workflow.yml'));
	await writeFile(join(shared, 'actionlint.yaml'), '{}\n');
	/** @type {Record<string, string>[]} */
	const externalInputs = [
		{ files: join(shared, 'workflow.yml') },
		{ files: relative(workspace, join(shared, 'workflow.yml')) },
		{ 'working-directory': shared, files: 'workflow.yml' },
		{ 'working-directory': relative(workspace, shared), files: 'workflow.yml' },
		{ 'config-file': join(shared, 'actionlint.yaml') },
		{ 'config-file': relative(workspace, join(shared, 'actionlint.yaml')) },
	];
	for (const inputs of externalInputs) await run(inputs);
	/** @type {Record<string, string>[]} */
	const invalidInputs = [
		{ format: 'invalid' },
		{ 'working-directory': 'missing-directory' },
		{ 'output-file': '../escaped.json' },
		{ 'output-file': '.' },
		{ files: '--help' },
		{ annotations: 'invalid' },
		{ sarif: 'invalid' },
	];
	for (const inputs of invalidInputs) await run(inputs, 2);
	await run({ 'config-file': join(shared, 'missing.yaml') }, 3);
	await writeFile(
		join(workspace, '.github/workflows/custom-runner.yml'),
		'name: Custom runner\non: push\njobs:\n  test:\n    runs-on: ubuntu-24.04-custom\n    steps:\n      - run: echo hello\n',
	);
	const customRunner = { files: '', shellcheck: 'false', pyflakes: 'false' };
	await run({ ...customRunner, config: JSON.stringify({ 'self-hosted-runner': { labels: ['ubuntu-24.04-custom'] } }) });
	await run({ ...customRunner, config: 'self-hosted-runner: {labels: [ubuntu-24.04-custom]}' });
	await run({ ...customRunner, ignore: 'label "ubuntu-24\\.04-custom" is unknown\\.' });
	const quotedIgnore = await run({ ...customRunner, ignore: '\'label "ubuntu-24\\.04-custom" is unknown\\.\'' }, 1);
	assert.match(quotedIgnore.log, /quotes|apostrophes/i);
	for (const extension of ['yaml', 'yml']) {
		const configuration = join(workspace, `.github/actionlint.${extension}`);
		await writeFile(configuration, 'self-hosted-runner:\n  labels: [ubuntu-24.04-custom]\n');
		const configured = await run(customRunner);
		assert.match(configured.log, new RegExp(`actionlint\\.${extension}`));
		await rm(configuration);
	}
	console.log('GitHub Action entrypoint tests passed');
} finally {
	if (dirname(workspace) === tempRoot) await rm(workspace, { recursive: true, force: true });
	if (dirname(shared) === tempRoot) await rm(shared, { recursive: true, force: true });
}
