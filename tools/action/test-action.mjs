// Exercise the published Node entrypoint and the ordinary actionlint binary with
// the same INPUT_* environment variables supplied by the Actions runner.
import assert from 'node:assert/strict';

import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { realpathSync } from 'node:fs';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const [actionDirectory, binary] = process.argv.slice(2);
if (!actionDirectory || !binary) throw new Error('Usage: test-action.mjs ACTION_DIRECTORY ACTIONLINT_BINARY');
const source = process.cwd();
const entrypoint = resolve(actionDirectory, 'action.mjs');
const tempRoot = resolve(tmpdir());
const workspace = await mkdtemp(join(tempRoot, 'actionlint-action-test-'));
const shared = await mkdtemp(join(tempRoot, 'actionlint-shared-test-'));
const candidate = join(shared, process.platform === 'win32' ? 'actionlint.exe' : 'actionlint');
const outputFile = join(workspace, 'github-output');
const summaryFile = join(workspace, 'github-summary');

/** @param {Record<string, string>} environment */
async function installManagedShell(environment) {
	const registry = join(workspace, 'managed-catalogue');
	const manifestDirectory = join(registry, 'plugins/shellcheck');
	await mkdir(join(manifestDirectory, 'versions'), { recursive: true });
	const contractDigest = createHash('sha256')
		.update(await readFile(join(source, 'crates/actionlint-extension-api/wit/analyzer.wit')))
		.digest('hex');
	const metadata = {
		metadata_version: 1,
		id: 'shellcheck',
		name: 'Managed ShellCheck fixture',
		description: 'Action integration fixture',
		license: 'MIT',
		plugin_version: '1.0.0',
		repository: 'https://github.com/publisher/managed-shellcheck',
		transport: 'wasm-component',
		interface: 'kjanat:actionlint/analyzer@0.1.0',
		contract_sha256: contractDigest,
	};
	const metadataFile = join(registry, 'metadata.json');
	const moduleFile = join(registry, 'plugin.wasm');
	await writeFile(metadataFile, JSON.stringify(metadata));
	const packageFixture = spawnSync('go', [
		'run',
		'./tools/plugin-fixture-metadata',
		join(source, 'internal/plugins/component/testdata/plugins/managed-no-sudo.wasm'),
		metadataFile,
		moduleFile,
	], { cwd: source, env: process.env, encoding: 'utf8' });
	assert.equal(
		packageFixture.status,
		0,
		`${packageFixture.error ?? ''}\n${packageFixture.stdout}\n${packageFixture.stderr}`,
	);
	const artifact = {
		kind: 'component',
		sha256: createHash('sha256').update(await readFile(moduleFile)).digest('hex'),
		plugin_id: metadata.id,
		plugin_version: metadata.plugin_version,
		repository: metadata.repository,
		contract: `${metadata.interface}#sha256=${contractDigest}`,
		metadata,
	};
	await writeFile(
		join(registry, 'index.json'),
		JSON.stringify({
			schema_version: 1,
			source: 'candidate',
			plugins: [{
				name: 'github.com/publisher/managed-shellcheck',
				plugin_id: metadata.id,
				manifest: 'plugins/shellcheck/index.json',
			}],
		}),
	);
	await writeFile(
		join(manifestDirectory, 'index.json'),
		JSON.stringify({
			source: 'candidate',
			name: 'github.com/publisher/managed-shellcheck',
			plugin_id: metadata.id,
			versions: [{ version: 'v1.0.0', record: 'versions/v1.json' }],
		}),
	);
	await writeFile(
		join(manifestDirectory, 'versions/v1.json'),
		JSON.stringify({ source: 'candidate', artifact, url: 'plugin.wasm' }),
	);
	await rm(join(workspace, '.github/actionlint.yaml'));
	await rm(join(workspace, '.github/actionlint.plugins.lock.json'));
	const install = spawnSync(candidate, [
		'plugin',
		'install',
		'publisher/managed-shellcheck@v1.0.0',
		'--registry',
		pathToFileURL(`${registry}/`).href,
	], { cwd: workspace, env: { ...process.env, ...environment }, encoding: 'utf8' });
	assert.equal(install.status, 0, `${install.error ?? ''}\n${install.stdout}\n${install.stderr}`);
}

/** @param {Record<string, string>} inputs @param {number} expected @param {Record<string, string>} extraEnvironment */
async function run(inputs, expected = 0, extraEnvironment = {}) {
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

			'add-actionlint-to-path': 'false',
			'working-directory': '.',
			'fail-on-error': 'true',
			...inputs,
		})
	) env[`INPUT_${name.toUpperCase()}`] = value;
	env.ACTIONLINT_ACTION_BINARY = candidate;
	env.GITHUB_WORKSPACE = workspace;
	env.GITHUB_OUTPUT = outputFile;
	env.GITHUB_STEP_SUMMARY = summaryFile;
	env.RUNNER_TEMP = workspace;
	env.GITHUB_ACTIONS = 'true';
	Object.assign(env, extraEnvironment);
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
	if (env.INPUT_FORMAT === 'json') assert.deepEqual(JSON.parse(outputs.get('output') || ''), analysis, log);
	return { outputs, log, analysis };
}

try {
	const binaryPath = realpathSync(resolve(binary));
	const buildRoot = realpathSync(process.env.RUNNER_TEMP || tempRoot);
	const allowedRoot = binaryPath.startsWith(buildRoot + sep) ? buildRoot : realpathSync(source);
	if (!binaryPath.startsWith(allowedRoot + sep)) {
		throw new Error('Candidate binary must be inside the project or runner build directory');
	}
	await copyFile(binaryPath, candidate);
	const gitEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_')));
	const init = spawnSync('git', ['init', '--quiet', workspace], { encoding: 'utf8', env: gitEnv });
	assert.equal(init.status, 0, `git init: ${init.error ?? init.stderr}`);
	await mkdir(join(workspace, '.github/workflows'), { recursive: true });
	for (
		const name of [
			'ok/minimal.yaml',
			'err/one_error.yaml',
			'err/shellcheck_default_shell_detection.yaml',
		]
	) {
		const destination = join(workspace, 'testdata', name);
		await mkdir(dirname(destination), { recursive: true });
		await copyFile(join(source, 'testdata', name), destination);
	}
	const clean = await run({});
	assert.equal(clean.analysis.sarif, undefined);
	for (
		const [name, expected] of Object.entries({
			'exit-code': '0',
			result: 'success',
			'problems-found': 'false',
			'problem-count': '0',
		})
	) {
		assert.equal(clean.outputs.get(name), expected, clean.log);
	}
	const cleanOutput = clean.outputs.get('output');
	assert.ok(cleanOutput);
	assert.deepEqual(JSON.parse(cleanOutput), clean.analysis);
	assert.match(clean.log, /0 problems in 1 workflow file \(requested tools: shellcheck, ruff\)/);
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
		const output = result.outputs.get('output');
		assert.ok(output);
		if (format === 'json') assert.deepEqual(JSON.parse(output), result.analysis);
		if (format === 'json-lines') {
			assert.deepEqual(JSON.parse(output), {
				schema_version: 1,
				...result.analysis.diagnostics[0],
			});
		}
		assert.equal(
			(result.log.match(/::error file=/g) || []).length,
			1,
			`duplicate or missing annotation: ${result.log}`,
		);
		const annotation = result.log.split('\n').find(line => line.startsWith('::error file='));
		assert.ok(annotation?.includes('%0A%0A6 |'), `annotation lacks numbered source: ${result.log}`);
		assert.match(annotation || '', /%0A {2}\| +\^~+/);
		const summary = await readFile(summaryFile, 'utf8');
		assert.doesNotMatch(summary, /\d+ errors? in \d+ workflows?/);
		assert.ok(summary.includes('<pre lang="yaml">'));
		assert.ok(summary.includes('    runs-on: ubuntu-latest&#10;    steps:&#10;'));
		assert.match(summary, /&#10; +\^~+/);
		assert.doesNotMatch(summary, /&#10;\d+ \| |&#10; *\|/);
		assert.ok(summary.includes('<code>github.event.head_commit.message</code>'));
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
	assert.match(await readFile(summaryFile, 'utf8'), /No findings/);
	const setupFailure = await run({ 'config-file': '.github/actionlint-missing.yaml', sarif: 'true' }, 3);
	assert.equal(setupFailure.outputs.get('exit-code'), '3');
	assert.equal(setupFailure.outputs.get('report-sarif'), '');
	assert.equal(setupFailure.analysis.completed, false);
	assert.match(await readFile(summaryFile, 'utf8'), /Could not start analysis/);
	assert.match(setupFailure.log, /::error::/);
	await run({ files: '' }, 3);
	// Tool booleans enable selected plugins; they do not install native tools.
	// Select the released plugin explicitly and forbid PATH-based fallbacks.
	const shellCache = join(workspace, 'shell-cache');
	await mkdir(shellCache);
	const shellEnvironment = { HOME: shellCache, XDG_CACHE_HOME: shellCache, LOCALAPPDATA: shellCache, PATH: '' };
	const installShell = spawnSync(candidate, ['plugin', 'install', 'shellcheck@v0.1.1'], {
		cwd: workspace,
		env: { ...process.env, ...shellEnvironment },
		encoding: 'utf8',
	});
	assert.equal(installShell.status, 0, `${installShell.error ?? ''}\n${installShell.stdout}\n${installShell.stderr}`);
	const shell = await run(
		{
			files: 'testdata/err/shellcheck_default_shell_detection.yaml',

			'fail-on-error': 'false',
		},
		0,
		shellEnvironment,
	);
	assert.equal(shell.outputs.get('problem-count'), '12', shell.log);
	await writeFile(
		join(workspace, 'shell-fix.yaml'),
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          value="$1"\n          echo $value\n',
	);
	const fixable = await run(
		{
			files: 'shell-fix.yaml',

			'fail-on-error': 'false',
			annotations: 'true',
			sarif: 'true',
		},
		0,
		shellEnvironment,
	);
	const githubFixable = await run(
		{
			files: 'shell-fix.yaml',

			'fail-on-error': 'false',
			annotations: 'true',
			format: 'github',
		},
		0,
		shellEnvironment,
	);
	const jsonAnnotations = fixable.log.match(/^::(?:error|warning|notice) file=.*$/gm) || [];
	const githubAnnotations = githubFixable.log.match(/^::(?:error|warning|notice) file=.*$/gm) || [];
	assert.ok(
		jsonAnnotations.some((line) => line.startsWith('::notice ') && line.includes('title=SC2086::')),
		fixable.log,
	);
	assert.deepEqual(githubAnnotations, jsonAnnotations);
	/** @type {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic[]} */
	const diagnostics = fixable.analysis.diagnostics;
	const quote = diagnostics.find((diagnostic) => diagnostic.code === 'SC2086');
	assert.ok(quote, fixable.log);
	assert.equal(quote.severity, 'info');
	assert.ok(quote.fixes && quote.fixes.length > 0);
	assert.ok(quote.fixes[0].edits.every((edit) => edit.path === 'shell-fix.yaml' && edit.start.line === 9));
	const sarifQuote = fixable.analysis.sarif.runs[0].results.find(
		/** @param {{properties?: {diagnosticCode?: string}}} diagnostic */
		(diagnostic) => diagnostic.properties?.diagnosticCode === 'SC2086',
	);
	assert.ok(sarifQuote);
	assert.equal(sarifQuote.level, 'note');
	assert.equal(fixable.analysis.sarif.runs[0].columnKind, 'unicodeCodePoints');
	assert.equal(sarifQuote.fixes[0].artifactChanges[0].artifactLocation.uri, 'shell-fix.yaml');
	assert.equal(sarifQuote.fixes[0].artifactChanges[0].replacements.length, quote.fixes[0].edits.length);
	const legacyStandalone = await run(
		{ files: 'shell-fix.yaml', shellcheck: 'false', 'fail-on-error': 'false' },
		0,
		shellEnvironment,
	);
	assert.ok(
		legacyStandalone.analysis.diagnostics.some(
			/** @param {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic} diagnostic */
			diagnostic => diagnostic.rule === 'shellcheck-rs',
		),
		legacyStandalone.log,
	);
	await installManagedShell(shellEnvironment);
	await writeFile(
		join(workspace, 'managed-shell.yaml'),
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n',
	);
	const managedShell = await run(
		{ files: 'managed-shell.yaml', 'fail-on-error': 'false' },
		0,
		shellEnvironment,
	);
	assert.equal(
		managedShell.analysis.diagnostics.filter(
			/** @param {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic} diagnostic */
			diagnostic => diagnostic.rule === 'shellcheck',
		).length,
		1,
		managedShell.log,
	);
	const noShell = await run(
		{
			files: 'managed-shell.yaml',
			shellcheck: 'false',
		},
		0,
		shellEnvironment,
	);
	assert.equal(noShell.outputs.get('problem-count'), '0', noShell.log);
	const configuredNoShell = await run(
		{
			files: 'managed-shell.yaml',
			config: 'tools: {shellcheck: {enabled: false}}',
		},
		0,
		shellEnvironment,
	);
	assert.equal(configuredNoShell.outputs.get('problem-count'), '0', configuredNoShell.log);
	await rm(join(workspace, '.github/actionlint.yaml'));
	await rm(join(workspace, '.github/actionlint.plugins.lock.json'));
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
		{ summary: 'bad' },
		{ config: 'tools: {shellchek: false}' },
	];
	for (const inputs of invalidInputs) await run(inputs, 2);
	await run({ files: 'missing.yml', shellcheck: 'false' }, 3);
	await run({ 'config-file': join(shared, 'missing.yaml') }, 3);
	await writeFile(
		join(workspace, '.github/workflows/custom-runner.yml'),
		'name: Custom runner\non: push\njobs:\n  test:\n    runs-on: ubuntu-24.04-custom\n    steps:\n      - run: echo hello\n',
	);
	const customRunner = { files: '', shellcheck: 'false' };
	await run({ ...customRunner, config: JSON.stringify({ 'self-hosted-runner': { labels: ['ubuntu-24.04-custom'] } }) });
	await run({ ...customRunner, config: 'self-hosted-runner: {labels: [ubuntu-24.04-custom]}' });
	await run({ ...customRunner, ignore: 'label "ubuntu-24\\.04-custom" is unknown\\.' });
	const quotedIgnore = await run({ ...customRunner, ignore: '\'label "ubuntu-24\\.04-custom" is unknown\\.\'' }, 1);
	assert.match(quotedIgnore.log, /quotes|apostrophes/i);
	for (const extension of ['yaml', 'yml']) {
		const configuration = join(workspace, `.github/actionlint.${extension}`);
		await writeFile(configuration, 'plugins: []\nself-hosted-runner:\n  labels: [ubuntu-24.04-custom]\n');
		const configured = await run(customRunner);
		assert.match(configured.log, new RegExp(`actionlint\\.${extension}`));
		await rm(configuration);
	}
	// Exercise released component bytes through the actual bundled launcher.
	// The second URL cannot download: success proves reuse of the digest cache.
	const pluginDigest = 'c33638bb1fac6167e4dcd3a5fb7f59fcaa639c93d2287c4dda07f2beea3522b4';
	const pluginURL = 'https://github.com/kjanat/actionlint-plugin-shellcheck/releases/download/v0.1.1/plugin.wasm';
	const pluginCache = join(workspace, 'plugin-cache');
	const pluginEnvironment = { HOME: pluginCache, XDG_CACHE_HOME: pluginCache, LOCALAPPDATA: pluginCache };
	await mkdir(pluginCache, { recursive: true });
	await writeFile(
		join(workspace, 'plugin.yml'),
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $GITHUB_SHA\n',
	);
	const realCache = process.env.ACTIONLINT_TEST_GITHUB_CACHE === '1';
	const pluginInputs = {
		files: 'plugin.yml',
		shellcheck: 'false',
		ruff: 'false',
		'plugin-cache': String(realCache),
	};
	/** @param {string} url */
	const pluginConfig = (url) =>
		JSON.stringify({
			plugins: [`${url}#sha256=${pluginDigest}`],
			lint: { rules: { plugins: { 'shellcheck-rs': 'error' } } },
		});
	const cold = await run({ ...pluginInputs, config: pluginConfig(pluginURL) }, 1, pluginEnvironment);
	/** @type {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic[]} */
	const coldDiagnostics = cold.analysis.diagnostics;
	assert.ok(coldDiagnostics.some((diagnostic) => diagnostic.code === 'SC2086'), cold.log);
	if (realCache) {
		// Remove only this test's verified artifact. The next process must restore
		// it from GitHub cache; a warning/miss cannot masquerade as coverage.
		const base = process.platform === 'darwin' ? join(pluginCache, 'Library/Caches') : pluginCache;
		await rm(join(base, 'actionlint/artifacts', pluginDigest));
	}
	const warm = await run(
		{
			...pluginInputs,
			config: pluginConfig('https://unavailable.invalid/plugin.wasm'),
		},
		1,
		pluginEnvironment,
	);
	assert.deepEqual(warm.analysis.diagnostics, cold.analysis.diagnostics);
	const install = spawnSync(candidate, ['plugin', 'install', 'shellcheck@v0.1.1'], {
		cwd: workspace,
		env: { ...process.env, ...pluginEnvironment },
		encoding: 'utf8',
	});
	assert.equal(install.status, 0, `${install.error ?? ''}\n${install.stdout}\n${install.stderr}`);
	const installed = await run(
		{ ...pluginInputs, shellcheck: 'true', 'fail-on-error': 'false' },
		0,
		pluginEnvironment,
	);
	/** @type {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic[]} */
	const installedDiagnostics = installed.analysis.diagnostics;
	assert.ok(
		installedDiagnostics.some((diagnostic) => diagnostic.code === 'SC2086' && diagnostic.rule === 'shellcheck-rs'),
		installed.log,
	);
	const disabled = await run(pluginInputs, 1, pluginEnvironment);
	/** @type {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic[]} */
	const disabledDiagnostics = disabled.analysis.diagnostics;
	assert.deepEqual(disabledDiagnostics, installedDiagnostics.filter(diagnostic => diagnostic.rule === 'shellcheck-rs'));
	const installRuff = spawnSync(candidate, ['plugin', 'install', 'ruff@v0.1.0'], {
		cwd: workspace,
		env: { ...process.env, ...pluginEnvironment },
		encoding: 'utf8',
	});
	assert.equal(installRuff.status, 0, `${installRuff.error ?? ''}\n${installRuff.stdout}\n${installRuff.stderr}`);
	await writeFile(
		join(workspace, 'default-plugins.yml'),
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $GITHUB_SHA\n      - shell: python\n        run: print(unknown_name)\n',
	);
	const defaultInputs = {
		files: 'default-plugins.yml',
		shellcheck: 'true',
		ruff: 'true',
		'plugin-cache': 'false',
		'fail-on-error': 'false',
	};
	const lockPath = join(workspace, '.github/actionlint.plugins.lock.json');
	const lock = await readFile(lockPath, 'utf8');
	const defaults = await run(defaultInputs, 0, pluginEnvironment);
	assert.equal(defaults.analysis.completed, true, defaults.log);
	/** @type {import('../../distribution/npm/facade/types/result.d.ts').Diagnostic[]} */
	const defaultDiagnostics = defaults.analysis.diagnostics;
	for (const [rule, code] of [['shellcheck-rs', 'SC2086'], ['ruff', 'F821']]) {
		assert.ok(
			defaultDiagnostics.some((diagnostic) => diagnostic.rule === rule && diagnostic.code === code),
			`missing ${rule}/${code}: ${defaults.log}`,
		);
	}
	// Both packages are pinned and cached now. Broken proxies make any registry
	// or artifact fetch fail; checking must use the recorded bytes unchanged.
	const offline = await run(defaultInputs, 0, {
		...pluginEnvironment,
		HTTP_PROXY: 'http://127.0.0.1:1',
		HTTPS_PROXY: 'http://127.0.0.1:1',
		http_proxy: 'http://127.0.0.1:1',
		https_proxy: 'http://127.0.0.1:1',
		NO_PROXY: '',
		no_proxy: '',
	});
	assert.equal(offline.analysis.completed, true, offline.log);
	assert.deepEqual(offline.analysis.diagnostics, defaultDiagnostics);
	assert.equal(await readFile(lockPath, 'utf8'), lock);
	console.log('GitHub Action entrypoint, published default plugins and cache tests passed');
} finally {
	if (dirname(workspace) === tempRoot) {
		await rm(workspace, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
	}
	if (dirname(shared) === tempRoot) await rm(shared, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
}
