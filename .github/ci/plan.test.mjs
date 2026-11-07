import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

import { YAML } from 'bun';

import { changedInputs, ciTrailers, matchesPaths, plan, readFilters, selectJobs } from './plan.mjs';

/** @param {import('node:test').TestContext} t */
function repository(t) {
	const cwd = mkdtempSync(join(tmpdir(), 'actionlint-ci-plan-'));
	t.after(() => rmSync(cwd, { recursive: true, force: true }));
	mkdirSync(join(cwd, '.github/ci'), { recursive: true });
	writeFileSync(
		join(cwd, '.github/ci/filters.yml'),
		`workflows:
  test:
    shared: []
    jobs:
      unit: ['**/*.go']
      nix: ['nix/**']
`,
	);
	const git = (/** @type {string[]} */ args) =>
		execFileSync('git', [
			'-c',
			'user.name=Test',
			'-c',
			'user.email=test@example.invalid',
			'-c',
			'commit.gpgsign=false',
			...args,
		], { cwd, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
	git(['init', '--quiet', '--initial-branch=base', '--template=']);
	/** @param {string} path @param {string} message */
	const commit = (path, message) => {
		mkdirSync(dirname(join(cwd, path)), { recursive: true });
		writeFileSync(join(cwd, path), message);
		git(['add', '--all']);
		git(['commit', '--quiet', '-m', message]);
		return git(['rev-parse', 'HEAD']);
	};
	const base = commit('README.md', 'Initial');
	const eventPath = join(cwd, 'event.json');
	/** @param {object} value */
	const event = value => {
		writeFileSync(eventPath, JSON.stringify(value));
	};
	const environment = { GITHUB_EVENT_NAME: 'pull_request', GITHUB_EVENT_PATH: eventPath };
	return { cwd, git, commit, base, event, environment };
}

/** @param {string} cwd */
function commandEnvironment(cwd) {
	const directory = join(cwd, '_runner_file_commands');
	mkdirSync(directory);
	const environment = {
		RUNNER_TEMP: cwd,
		GITHUB_OUTPUT: join(directory, 'set_output_test'),
		GITHUB_STEP_SUMMARY: join(directory, 'step_summary_test'),
	};
	writeFileSync(environment.GITHUB_OUTPUT, '');
	writeFileSync(environment.GITHUB_STEP_SUMMARY, '');
	return environment;
}

test('paths are rooted, * does not cross directories, and ** does', () => {
	assert.equal(matchesPaths('README.md', ['*.md']), true);
	assert.equal(matchesPaths('docs/README.md', ['*.md']), false);
	assert.equal(matchesPaths('docs/a/file.md', ['docs/*']), false);
	assert.equal(matchesPaths('docs/a/file.md', ['docs/**']), true);
	assert.equal(matchesPaths('index.js', ['**.js']), true);
	assert.equal(matchesPaths('src/index.js', ['**.js']), false);
	assert.equal(matchesPaths('src/index.js', ['**/*.js']), true);
	assert.equal(matchesPaths('README.md', ['**/README.md']), true);
	assert.equal(matchesPaths('docs/README.md', ['**/README.md']), true);
	assert.equal(matchesPaths('.github/workflows/ci.yml', ['.github/workflows/**']), true);
	assert.equal(matchesPaths('page.jsx', ['*.js?']), true);
	assert.equal(matchesPaths('.github/actions/setup/main.mjs', ['**/*.{js,mjs,ts}']), true);
	assert.equal(matchesPaths('internal/cli/check.go', ['{cmd,internal/cli}/**']), true);
});

test('ordered exclusions and re-inclusions apply to each changed path independently', () => {
	const patterns = ['*.md', '!README.md', 'README*'];
	assert.equal(matchesPaths('README.md', patterns), true);
	assert.equal(matchesPaths('README.doc', patterns), true);
	assert.equal(matchesPaths('docs/hello.md', patterns), false);
	const filters = { shared: [], jobs: { unit: ['**/*.go', '!testdata/**'] } };
	assert.deepEqual(selectJobs(filters, ['testdata/fixture.go']), { unit: false });
	assert.deepEqual(selectJobs(filters, ['testdata/fixture.go', 'internal/cli/check.go']), { unit: true });
});

test('only actual trailers control optional groups; full overrides skips', () => {
	assert.deepEqual(ciTrailers('Mention CI-Skip: nix in the subject'), { full: false, skip: [] });
	assert.deepEqual(ciTrailers('Subject\n\nCI-Skip: nix, conformance\nCI-Skip: expression-probe\n'), {
		full: false,
		skip: ['nix', 'conformance', 'expression-probe'],
	});
	assert.deepEqual(ciTrailers('Subject\n\nCI-Skip: nix\nCI: full\n'), { full: true, skip: [] });
	assert.throws(() => ciTrailers('Subject\n\nCI-Skip: unit\n'), /optional/);
	assert.throws(() => ciTrailers('Subject\n\nCI: none\n'), /Unsupported/);
});

test('PR comparison uses the merge-base and latest head message, not the merge commit', t => {
	const repo = repository(t);
	repo.git(['checkout', '--quiet', '-b', 'pr']);
	repo.commit('internal/cli/check.go', 'First PR change\n\nCI-Skip: conformance');
	const head = repo.commit(
		'internal/development/tests/integration/cli/check_test.go',
		'Latest PR change\n\nCI-Skip: nix',
	);
	repo.git(['checkout', '--quiet', 'base']);
	const base = repo.commit('unrelated.go', 'Base advanced');
	repo.git(['merge', '--quiet', '--no-ff', 'pr', '-m', 'Synthetic merge\n\nCI: full']);
	repo.event({ pull_request: { base: { sha: base }, head: { sha: head } } });
	assert.deepEqual(changedInputs(repo.environment, repo.cwd), {
		paths: ['internal/cli/check.go', 'internal/development/tests/integration/cli/check_test.go'],
		trailers: { full: false, skip: ['nix'] },
	});
});

test('push comparison includes every pushed commit and ignores skip trailers', t => {
	const repo = repository(t);
	repo.commit('first.go', 'First\n\nCI-Skip: nix');
	const head = repo.commit('second.go', 'Second\n\nCI-Skip: conformance');
	repo.event({ before: repo.base, after: head });
	assert.deepEqual(changedInputs({ ...repo.environment, GITHUB_EVENT_NAME: 'push' }, repo.cwd), {
		paths: ['first.go', 'second.go'],
		trailers: { full: false, skip: [] },
	});
});

test('renames retain both old and new names, and deletions still select their checks', t => {
	const repo = repository(t);
	const base = repo.commit('old/check.go', 'Add source');
	repo.git(['mv', 'old/check.go', 'renamed.go']);
	repo.git(['rm', 'README.md']);
	repo.git(['commit', '--quiet', '-m', 'Move and delete']);
	repo.event({ pull_request: { base: { sha: base }, head: { sha: repo.git(['rev-parse', 'HEAD']) } } });
	assert.deepEqual(changedInputs(repo.environment, repo.cwd).paths, ['README.md', 'old/check.go', 'renamed.go']);
});

test('missing history or invalid skip requests run all checks and explain the fallback', t => {
	const repo = repository(t);
	const head = repo.commit('docs/usage.md', 'Docs\n\nCI-Skip: unit');
	const commands = commandEnvironment(repo.cwd);
	const environment = {
		...repo.environment,
		...commands,
		CI_PROFILE: 'test',
	};
	for (const base of [repo.base, 'f'.repeat(40)]) {
		repo.event({ pull_request: { base: { sha: base }, head: { sha: head } } });
		assert.deepEqual(plan(environment, repo.cwd), { unit: true, nix: true });
	}
	assert.match(readFileSync(commands.GITHUB_STEP_SUMMARY, 'utf8'), /running every check/);
	assert.match(readFileSync(commands.GITHUB_OUTPUT, 'utf8'), /jobs=\{"unit":true,"nix":true\}/);
});

test('runner command files cannot escape through traversal, sibling prefixes or symlinks', t => {
	const repo = repository(t);
	repo.event({});
	const commands = commandEnvironment(repo.cwd);
	const sibling = join(repo.cwd, '_runner_file_commands-other');
	mkdirSync(sibling);
	const outside = join(sibling, 'report');
	writeFileSync(outside, 'unchanged');
	const link = join(repo.cwd, '_runner_file_commands', 'linked');
	symlinkSync(outside, link);
	const traversal = `${dirname(commands.GITHUB_OUTPUT)}/../_runner_file_commands-other/report`;
	for (const name of ['GITHUB_OUTPUT', 'GITHUB_STEP_SUMMARY']) {
		for (const path of [outside, traversal, link]) {
			assert.throws(() =>
				plan({
					...repo.environment,
					...commands,
					GITHUB_EVENT_NAME: 'workflow_dispatch',
					CI_PROFILE: 'test',
					[name]: path,
				}, repo.cwd), /runner command directory/);
			assert.equal(readFileSync(outside, 'utf8'), 'unchanged');
		}
	}
});

test('manual and scheduled runs select all jobs', t => {
	const repo = repository(t);
	repo.event({});
	for (const event of ['workflow_dispatch', 'schedule']) {
		const { paths, trailers } = changedInputs({ ...repo.environment, GITHUB_EVENT_NAME: event }, repo.cwd);
		assert.deepEqual(selectJobs({ shared: [], jobs: { nix: ['nix/**'], unit: ['**/*.go'] } }, paths, trailers), {
			nix: true,
			unit: true,
		});
	}
});

/** @param {string} workflow */
function workflowFilters(workflow) {
	const text = readFileSync(new URL(`../workflows/${workflow}.yml`, import.meta.url), 'utf8');
	assert.match(text, new RegExp(`profile: ${workflow}\\b`), `${workflow} must select its input profile`);
	return readFilters(workflow, resolve(import.meta.dirname, '../..'));
}

test('shared selector and task changes select every check in every profile', () => {
	for (const workflow of ['ci', 'codeql', 'conformance', 'matcher', 'expression-probe-tests']) {
		for (const path of ['.github/ci/filters.yml', '.mise.toml', 'tasks.toml']) {
			assert.ok(Object.values(selectJobs(workflowFilters(workflow), [path])).every(Boolean), `${workflow}: ${path}`);
		}
	}
});

test('missing profiles and malformed path lists fail instead of silently skipping checks', t => {
	const repo = repository(t);
	assert.throws(() => readFilters('missing', repo.cwd), /Invalid CI path filters/);
	for (
		const source of [
			'workflows: {test: {shared: [42], jobs: {unit: ["**/*.go"]}}}',
			'workflows: {test: {shared: [], jobs: {unit: ["!"]}}}',
			'workflows: {test: {shared: [], jobs: {Unit: ["**/*.go"]}}}',
		]
	) {
		writeFileSync(join(repo.cwd, '.github/ci/filters.yml'), source);
		assert.throws(() => readFilters('test', repo.cwd), /CI (path|check)/);
	}
});

test('plugin sources and Cargo inputs select the plugin checks', () => {
	const ci = workflowFilters('ci');
	for (
		const path of [
			'Cargo.toml',
			'Cargo.lock',
			'rust-toolchain.toml',
			'plugins/examples/pipe-to-shell/src/lib.rs',
			'plugins/sdk/rust/Cargo.toml',
			'plugins/sdk/rust/build.rs',
			'plugins/sdk/rust/src/lib.rs',
			'plugins/sdk/rust/src/package.rs',
			'plugins/sdk/rust/wit/analyzer.wit',
			'plugins/sdk/rust/wit/source-annotations.wit',
			'internal/plugins/execution/component/testdata/sources/misbehave/src/lib.rs',
			'internal/model/runners/testdata/provider/snapshot.json',
			'internal/development/tests/conformance/wasm/runner-host/src/main.rs',
			'internal/development/tests/conformance/wasm/runner-host/Cargo.toml',
			'internal/development/tests/conformance/wasm/runner-host/Cargo.lock',
			'internal/development/generators/plugin-contract/main.go',
			'internal/development/generators/plugin-contract/bindings.go',
			'internal/checking/execute.go',
			'internal/checking/session_document.go',
			'internal/plugins/execution/load.go',
			'internal/development/tests/integration/checking/plugins_test.go',
		]
	) {
		assert.equal(selectJobs(ci, [path]).plugins, true, path);
	}
});

test('runner sources select every Go consumer and provider tests select their test jobs', () => {
	const ci = workflowFilters('ci');
	const jobs = selectJobs(ci, ['internal/model/runners/knowledge.go']);
	assert.deepEqual(Object.keys(jobs).filter(job => jobs[job]), [
		'unit',
		'race',
		'nix',
		'wasm',
		'release',
		'lint',
		'docker',
		'action',
		'plugins',
	]);
	const tests = selectJobs(ci, ['internal/model/runners/knowledge_test.go']);
	assert.deepEqual(Object.keys(tests).filter(job => tests[job]), ['unit', 'race', 'nix', 'lint', 'plugins']);
});

test('test-only CLI changes select Go and Nix tests without release builds or unrelated suites', () => {
	const jobs = selectJobs(workflowFilters('ci'), ['internal/development/tests/integration/cli/usability_test.go']);
	assert.deepEqual(Object.keys(jobs).filter(job => jobs[job]), ['unit', 'race', 'nix', 'lint']);
	for (const workflow of ['codeql', 'conformance', 'expression-probe-tests', 'matcher']) {
		assert.ok(
			Object.values(
				selectJobs(workflowFilters(workflow), ['internal/development/tests/integration/cli/usability_test.go']),
			).every(run => !run),
		);
	}
});

test('Nix checks include test sources and fixtures used by its Go suite', () => {
	const ci = workflowFilters('ci');
	for (
		const path of [
			'internal/development/tests/integration/checking/plugins_test.go',
			'internal/plugins/execution/module/testdata/engines/echo.wasm',
			'internal/development/generators/config-schema/main_test.go',
			'internal/development/generators/config-schema/testdata/example.yaml',
			'internal/development/tests/integration/fuzz/fuzz_test.go',
			'internal/development/tests/integration/workflow/testdata/ok/minimal.yaml',
		]
	) {
		assert.equal(selectJobs(ci, [path]).nix, true, path);
	}
});

test('action tests, probe results, mixed documentation and source changes select their owners', () => {
	const ci = workflowFilters('ci');
	const action = selectJobs(ci, ['applications/github-action/tests/reporters.test.ts']);
	assert.deepEqual(Object.keys(action).filter(job => action[job]), ['javascript', 'action', 'action-tests']);
	const documentation = selectJobs(ci, ['docs/usage.md']);
	assert.deepEqual(Object.keys(documentation).filter(job => documentation[job]), ['release']);
	assert.equal(selectJobs(ci, ['docs/usage.md', 'config.go']).unit, true);
	assert.equal(
		selectJobs(workflowFilters('expression-probe-tests'), [
			'internal/development/tests/conformance/expressions/results/run.json',
		])['expression-probe'],
		false,
	);
	assert.equal(selectJobs(workflowFilters('conformance'), ['internal/cli/check.go']).conformance, false);
	assert.equal(selectJobs(workflowFilters('conformance'), ['rule_expression.go']).conformance, true);
	for (
		const file of [
			'internal/development/tests/conformance/conformance_test.go',
			'internal/development/tests/conformance/conformance_adapters_test.go',
			'internal/development/tests/conformance/conformance_score_test.go',
			'internal/development/tests/conformance/conformance_assessments_test.go',
			'internal/development/tests/conformance/testdata/assessments.json',
			'internal/development/tests/conformance/testdata/differences.json',
		]
	) {
		assert.equal(selectJobs(workflowFilters('conformance'), [file]).conformance, true, file);
	}
	assert.equal(selectJobs(workflowFilters('matcher'), ['internal/cli/check.go']).matcher, true);
	assert.equal(selectJobs(ci, ['internal/cli/check.go'])['action-tests'], false);
	assert.equal(selectJobs(ci, ['distribution/docker/Dockerfile'])['action-tests'], false);
	assert.equal(selectJobs(ci, ['applications/github-action/tests/check-action-outputs.mjs'])['action-tests'], true);
	assert.equal(selectJobs(ci, ['internal/development/generators/config-schema/main.go']).unit, true);
	assert.equal(selectJobs(ci, ['internal/development/generators/config-schema/main.go']).lint, true);
	assert.equal(selectJobs(ci, ['tools/generate/webhook-payloads/main.mts']).javascript, true);
	assert.equal(selectJobs(ci, ['.github/workflows/upkeep.yml']).unit, true);
	assert.ok(Object.values(selectJobs(ci, ['.github/workflows/ci.yml'])).every(Boolean));
});

test('Action smoke fixtures are re-included after the general testdata exclusion', () => {
	const ci = workflowFilters('ci');
	for (
		const path of [
			'internal/development/tests/integration/workflow/testdata/ok/minimal.yaml',
			'internal/development/tests/integration/workflow/testdata/err/one_error.yaml',
		]
	) {
		assert.equal(selectJobs(ci, [path]).action, true, path);
	}
	assert.equal(
		selectJobs(ci, ['internal/development/tests/integration/workflow/testdata/ok/other.yaml']).action,
		false,
	);
});

test('Pandoc pins and installer select both consumers and installation regression tests', () => {
	for (const path of ['.github/actions/setup-pandoc/SHA256SUMS', '.github/actions/setup-pandoc/setup.bash']) {
		const jobs = selectJobs(workflowFilters('ci'), [path]);
		assert.equal(jobs.javascript, true);
		assert.equal(jobs.release, true);
		assert.equal(jobs.wasm, true);
	}
	const workflow = readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8');
	const wasm = workflow.split('\n  wasm:\n')[1]?.split(/\n {2}\S/)[0];
	assert.ok(wasm);
	assert.match(wasm, /uses: \$\/\.github\/actions\/setup-pandoc/);
	assert.doesNotMatch(wasm, /install_args:.*github:jgm\/pandoc/);
});

test('runner cache metadata and code select the installation regression tests', () => {
	for (const path of ['action.yml', 'setup.mjs', 'setup.test.mjs', 'mise.toml']) {
		assert.equal(selectJobs(workflowFilters('ci'), [`.github/actions/setup-runner/${path}`]).javascript, true, path);
	}
});

test('Wasm toolchain pins and installer select plugin and installation tests', () => {
	for (const path of ['releases.json', 'setup.bash', 'action.yml', 'setup.test.mjs', 'mise.toml']) {
		const jobs = selectJobs(workflowFilters('ci'), [`.github/actions/setup-wasm-toolchain/${path}`]);
		assert.equal(jobs.javascript, true, path);
		assert.equal(jobs.plugins, true, path);
	}
});

test('release snapshots cover every archived documentation input', () => {
	const ci = workflowFilters('ci');
	for (
		const path of ['README.md', 'LICENSE.txt', 'docs/usage.md', 'docs/releases/v1.18.0.md', 'docs/testdata/example.md']
	) {
		assert.equal(selectJobs(ci, [path]).release, true, path);
	}
	assert.equal(selectJobs(ci, ['CONTRIBUTING.md']).release, false);
});

test('Go module checks follow the guest SDK and Go example locations', () => {
	const ci = workflowFilters('ci');
	for (
		const path of [
			'plugins/sdk/go/go.mod',
			'plugins/sdk/go/bindings/generated.go',
			'plugins/sdk/go/packaging/package.go',
			'plugins/sdk/go/packaging/analyzer.wit',
			'plugins/examples/insecure-download/main.go',
			'plugins/examples/insecure-download/main_native.go',
			'go.mod',
		]
	) {
		assert.equal(selectJobs(ci, [path])['guest-modules'], true, path);
	}
	for (
		const path of [
			'distribution/npm/src/build.ts',
			'.github/actions/npm-packages/action.yml',
			'internal/checking/session.go',
		]
	) {
		assert.equal(selectJobs(ci, [path])['guest-modules'], false, path);
	}
});

test('Windows matrix jobs serialize setup to avoid shared event payload writes', () => {
	const workflow = readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8');
	for (const job of ['unit-tests', 'action']) {
		const body = workflow.split(`\n  ${job}:\n`)[1]?.split(/\n {2}\S/)[0];
		assert.ok(body, `${job} job must exist`);
		assert.match(body, /windows-/);
		assert.doesNotMatch(body, /^\s+- parallel:/m, `${job} steps share the runner event.json file`);
	}
});

test('workflow changes select installer tests', () => {
	const root = new URL('../../', import.meta.url);
	const read = (/** @type {string} */ path) => readFileSync(new URL(path, root), 'utf8');
	const pages = read('.github/workflows/pages.yml');
	assert.match(pages, /uses: \$\/\.github\/actions\/setup-runner/);
	assert.match(pages, /uses: \$\/\.github\/actions\/setup-pandoc/);
	assert.doesNotMatch(pages, /install_args:.*(?:runner|pandoc)/);
	const upkeep = read('.github/workflows/upkeep.yml');
	assert.match(upkeep, /uses: \$\/\.github\/actions\/setup-shellcheck/);
	assert.match(
		upkeep,
		/MISE_DISABLE_TOOLS: "shellcheck,github:koalaman\/shellcheck,rshellcheck,github:kjanat\/shellcheck-rs"/,
	);
	assert.doesNotMatch(upkeep, /install_args:.*koalaman\/shellcheck/);
});

test('browser server changes select browser tests and Pages preserves ordered exclusions', () => {
	const ci = workflowFilters('ci');
	for (
		const path of [
			'libraries/browser-lsp/src/worker.ts',
			'cmd/actionlint-lsp-wasm/main.go',
			'applications/playground/src/client.ts',
		]
	) {
		assert.equal(selectJobs(ci, [path]).wasm, true, path);
	}
	const pages = YAML.parse(readFileSync(new URL('../workflows/pages.yml', import.meta.url), 'utf8'));
	assert.ok(typeof pages === 'object' && pages !== null && 'on' in pages);
	const triggers = pages.on;
	assert.ok(typeof triggers === 'object' && triggers !== null && 'push' in triggers);
	const push = triggers.push;
	assert.ok(typeof push === 'object' && push !== null && 'paths' in push);
	const paths = push.paths;
	assert.ok(Array.isArray(paths) && paths.every((path) => typeof path === 'string'));
	for (
		const path of ['libraries/browser-lsp/src/worker.ts', 'cmd/actionlint-lsp-wasm/main.go', 'man/actionlint.1.md']
	) {
		assert.equal(matchesPaths(path, paths), true, path);
	}
	for (
		const path of [
			'cmd/actionlint/main.go',
			'internal/cli/app.go',
			'internal/lsp/lsp_test.go',
			'applications/playground/README.md',
		]
	) {
		assert.equal(matchesPaths(path, paths), false, path);
	}
});

test('autofix installs the Node.js tools for table and README generation', () => {
	const workflow = readFileSync(new URL('../workflows/autofix.yml', import.meta.url), 'utf8');
	const steps = workflow.split(/\n {6}- /);
	const bootstraps = steps.filter(step => /uses: jdx\/mise-action@/.test(step));
	assert.equal(bootstraps.length, 1, 'README generation requires one explicit mise bootstrap');
	const [bootstrap] = bootstraps;
	assert.ok(bootstrap);
	assert.doesNotMatch(bootstrap, /\n\s+if:/);
	assert.match(
		bootstrap,
		/install: \$\{\{ steps\.inputs\.outputs\.tables == 'true' \|\| steps\.inputs\.outputs\.readme == 'true' \}\}/,
	);
	assert.match(bootstrap, /env: true/);
	assert.match(bootstrap, /add_shims_to_path: false/);
	assert.match(
		bootstrap,
		/working_directory: \$\{\{ steps\.inputs\.outputs\.tables == 'true' && github\.workspace \|\| runner\.temp \}\}/,
	);
	assert.match(bootstrap, /mise_dir: \$\{\{ env\.ACTIONLINT_MISE_DIR \}\}/);
	assert.match(bootstrap, /install_args: node npm bun/);
	assert.match(bootstrap, /MISE_ENABLE_TOOLS: "node,npm,bun"/);
	assert.doesNotMatch(workflow, /uses: actions\/setup-node@/);
	const readme = steps.findIndex(step => /id: readme\n/.test(step));
	assert.ok(readme > steps.indexOf(bootstrap), 'mise must exist before the README generator runs');
});

/** @param {unknown} value @returns {Generator<Record<string, unknown>>} */
function* yamlObjects(value) {
	if (Array.isArray(value)) {
		for (const item of value) yield* yamlObjects(item);
	} else if (value !== null && typeof value === 'object') {
		/** @type {Record<string, unknown>} */
		const fields = Object.fromEntries(Object.entries(value));
		yield fields;
		for (const item of Object.values(fields)) yield* yamlObjects(item);
	}
}

/** @param {unknown} value */
function yamlMapping(value) {
	assert.ok(value !== null && typeof value === 'object' && !Array.isArray(value));
	/** @type {Record<string, unknown>} */
	const fields = Object.fromEntries(Object.entries(value));
	return fields;
}

test('mise setup limits environment resolution to explicitly installed tools', () => {
	const directory = new URL('../workflows/', import.meta.url);
	for (const name of readdirSync(directory).filter(name => /\.ya?ml$/.test(name))) {
		/** @type {unknown} */
		const workflow = YAML.parse(readFileSync(new URL(name, directory), 'utf8'));
		for (const step of yamlObjects(workflow)) {
			if (typeof step.uses !== 'string' || !step.uses.startsWith('jdx/mise-action@')) continue;
			const options = yamlMapping(step.with);
			const args = options.install_args;
			assert.equal(options.add_shims_to_path, false, `${name} must export concrete tool paths`);
			if (typeof args === 'string' && args.trim()) {
				const environment = yamlMapping(step.env);
				assert.equal(
					environment.MISE_ENABLE_TOOLS,
					args.trim().replaceAll(/\s+/g, ','),
					`${name} selected tool environment`,
				);
			} else {
				assert.equal(options.install, false, `${name} bootstrap must not install configured tools`);
				assert.equal(options.env, false, `${name} bootstrap must not resolve the project environment`);
				assert.equal(options.working_directory, '${{ runner.temp }}', `${name} bootstrap must not list project tools`);
			}
		}
	}
});

test('preview selects the Go toolchain and npm assembly uses Node and Bun', () => {
	const preview = readFileSync(new URL('../workflows/annotation-preview.yml', import.meta.url), 'utf8');
	assert.match(preview, /GOTOOLCHAIN: local/);
	assert.match(preview, /with: \{ go-version-file: go.mod \}\n\s*#[^\n]*\n\s*env: \{ GOTOOLCHAIN: auto \}/);
	const npm = readFileSync(new URL('../workflows/npm-release.yml', import.meta.url), 'utf8');
	assert.doesNotMatch(npm, /uses: actions\/setup-go@/);
	assert.match(npm, /node-version-file: \.node-version/);
	assert.match(npm, /install_args: bun/);
	assert.match(npm, /bun install --filter @kjanat\/actionlint-npm --frozen-lockfile --ignore-scripts/);
});

test('standalone Rust host changes select plugin parity without Go or packaging matrices', () => {
	for (const file of ['src/main.rs', 'Cargo.toml', 'Cargo.lock']) {
		const path = `internal/development/tests/conformance/wasm/runner-host/${file}`;
		const jobs = selectJobs(workflowFilters('ci'), [path]);
		assert.deepEqual(Object.keys(jobs).filter(job => jobs[job]), ['plugins'], path);
		assert.equal(selectJobs(workflowFilters('matcher'), [path]).matcher, false, path);
		assert.equal(selectJobs(workflowFilters('conformance'), [path]).conformance, true, path);
	}
});

test('Go consumers include embedded assets and checked-in guest fixtures', () => {
	for (
		const path of [
			'internal/input/configuration/config_default.yaml',
			'internal/input/configuration/schema/assets/actionlint.schema.json',
			'internal/model/contexts/events/payload/catalogue.json',
			'internal/plugins/manifests/manifest_schema.json',
			'internal/plugins/execution/process/result_schema.json',
			'internal/plugins/manifests/bundled/ruff/output.jq',
			'internal/development/tests/conformance/sources.json',
			'internal/development/generators/event-triggers/sources/events.tsv',
			'plugins/sdk/go/packaging/analyzer.wit',
			'plugins/protocol/wit/analyzer.wit',
			'internal/plugins/execution/component/testdata/plugins/no-sudo.wasm',
		]
	) {
		const jobs = selectJobs(workflowFilters('ci'), [path]);
		assert.equal(jobs.unit, true, path);
		assert.equal(jobs.race, true, path);
		assert.equal(jobs.nix, true, path);
	}
	const source = selectJobs(workflowFilters('ci'), [
		'internal/plugins/execution/component/testdata/sources/misbehave/src/lib.rs',
	]);
	assert.deepEqual(Object.keys(source).filter(job => source[job]), ['plugins']);
});

test('expensive CI jobs wait for selected checks while unselected checks can be skipped', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8')));
	const jobs = yamlMapping(workflow.jobs);
	const checks = yamlMapping(jobs.checks);
	assert.deepEqual(checks.needs, ['lint', 'javascript', 'go-guest-modules', 'rust-native']);
	assert.equal(checks.if, '${{ !cancelled() }}');
	const steps = checks.steps;
	assert.ok(Array.isArray(steps));
	assert.equal(yamlMapping(steps[0]).run, 'test "$CHECKS_PASSED" = true');
	assert.equal(
		yamlMapping(yamlMapping(steps[0]).env).CHECKS_PASSED,
		"${{ !contains(needs.*.result, 'failure') && !contains(needs.*.result, 'cancelled') }}",
	);
	for (const name of checks.needs) {
		assert.deepEqual(yamlMapping(jobs[name]).needs, 'changes', `${name} must run before the matrix`);
	}
	for (const name of ['unit-tests', 'race', 'nix', 'wasm', 'release-snapshot', 'plugins', 'docker', 'action']) {
		const job = yamlMapping(jobs[name]);
		assert.deepEqual(job.needs, ['changes', 'checks'], name);
		assert.ok(typeof job.if === 'string');
		assert.ok(job.if.startsWith("${{ !cancelled() && needs.checks.result == 'success'"), name);
	}
	const unit = yamlMapping(jobs['unit-tests']);
	assert.deepEqual(yamlMapping(yamlMapping(unit.strategy).matrix).os, [
		'ubuntu-26.04',
		'ubuntu-26.04-arm',
		'macos-latest',
		'macos-26-intel',
		'windows-2025-vs2026',
		'windows-11-vs2026-arm',
	]);
	assert.ok(typeof unit['runs-on'] === 'string' && unit['runs-on'].endsWith("|| 'ubuntu-slim' }}"));
});

test('native Rust tests run once before component builds, with separate writable build caches', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8')));
	const jobs = yamlMapping(workflow.jobs);
	const native = yamlMapping(jobs['rust-native']);
	const component = yamlMapping(jobs.plugins);
	assert.equal(
		native.if,
		"${{ !cancelled() && (needs.changes.result != 'success' || fromJSON(needs.changes.outputs.jobs)['plugins']) }}",
	);
	/** @param {Record<string, unknown>} job */
	const commands = (job) => {
		assert.ok(Array.isArray(job.steps));
		return job.steps.map(step => yamlMapping(step)).flatMap(step => typeof step.run === 'string' ? [step.run] : []);
	};
	const early = commands(native);
	const late = commands(component);
	assert.ok(early.includes('mise run plugins-check-native'));
	assert.ok(
		early.includes(
			'cargo check --locked --all-targets --manifest-path internal/development/tests/conformance/wasm/runner-host/Cargo.toml',
		),
	);
	assert.ok(!late.includes('mise run plugins-check-native'));
	for (
		const command of [
			'cargo package --locked -p actionlint-extension-api --all-features',
			'cargo build --locked --manifest-path internal/development/tests/conformance/wasm/runner-host/Cargo.toml',
			'mise run --force plugins',
		]
	) assert.ok(late.includes(command), command);
	assert.ok(late.some(command => command.includes('go test -count=1 -v ./internal/plugins/execution/component')));
	/** @param {Record<string, unknown>} job */
	const cache = job => {
		assert.ok(Array.isArray(job.steps));
		const step = job.steps.map(step => yamlMapping(step)).find(step =>
			typeof step.uses === 'string' && step.uses.startsWith('actions/cache@')
		);
		assert.ok(step);
		return yamlMapping(step.with);
	};
	assert.equal(cache(native).path, cache(component).path);
	assert.notEqual(cache(native).key, cache(component).key);
	const fallback = cache(component)['restore-keys'];
	assert.ok(typeof fallback === 'string' && fallback.includes('cargo-native-'));
});
