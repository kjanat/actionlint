import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import {
	appendFileSync,
	existsSync,
	mkdirSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	rmSync,
	symlinkSync,
	writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { runInNewContext } from 'node:vm';

import { YAML } from 'bun';

import { affectedProjects, changedInputs, ciTrailers, jobNames, plan, selectJobs } from './plan.mjs';

const root = resolve(import.meta.dirname, '../..');

test('mise follows workspace dependencies and isolates the standalone reference host', { timeout: 120000 }, t => {
	const repo = repository(t);
	const manifests = execFileSync('git', [
		'ls-files',
		'-z',
		'--',
		'Cargo.toml',
		'package.json',
		'.mise.toml',
		'tasks.toml',
		'**/Cargo.toml',
		'**/package.json',
		'**/mise.toml',
	], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
	for (const file of manifests) {
		if (!existsSync(join(root, file))) continue;
		mkdirSync(dirname(join(repo.cwd, file)), { recursive: true });
		writeFileSync(join(repo.cwd, file), readFileSync(join(root, file)));
	}
	const base = repo.commit('fixture.txt', 'Workspace manifests');
	const names = jobNames('ci', root);
	const conformance = jobNames('conformance', root);
	const selected = (/** @type {string} */ file) => {
		const head = repo.commit(file, `Change ${file}`);
		const projects = affectedProjects(base, head, repo.cwd);
		// Start the next scenario from the workspace-manifest commit.
		repo.git(['checkout', '--quiet', '--detach', base]);
		return { projects, jobs: selectJobs('ci', names, projects) };
	};
	for (const file of ['src/main.rs', 'Cargo.toml', 'Cargo.lock']) {
		const { projects, jobs } = selected(`internal/development/tests/conformance/wasm/runner-host/${file}`);
		assert.deepEqual(projects, ['custom:runner-host'], file);
		assert.deepEqual(Object.keys(jobs).filter(job => jobs[job]), ['plugins'], file);
		assert.equal(selectJobs('conformance', conformance, projects).conformance, true);
	}
	const sdk = selected('plugins/sdk/typescript/src/test-change.ts');
	assert.ok(sdk.projects.includes('node:@kjanat-actionlint/plugin-yamllint'));
	assert.ok(sdk.projects.includes('node:@kjanat-actionlint/plugin-grype'));
	assert.equal(sdk.jobs.javascript, true);
	assert.equal(sdk.jobs.plugins, true);
	assert.equal(sdk.jobs.unit, false);
	const rust = selected('plugins/sdk/rust/src/test_change.rs');
	assert.ok(rust.projects.includes('cargo:actionlint-example-pinned-actions'));
	assert.equal(rust.jobs.plugins, true);
	assert.equal(rust.jobs.unit, false);
	const protocol = selected('plugins/protocol/wit/test-change.wit');
	for (const job of ['unit', 'race', 'nix', 'plugins', 'javascript', 'guest-modules']) {
		assert.equal(protocol.jobs[job], true, job);
	}
	for (
		const file of [
			'internal/input/configuration/schema/assets/test-change.json',
			'internal/plugins/execution/component/testdata/plugins/test-change.wasm',
			'internal/checking/test_change.go',
			'schemas/test-change.json',
		]
	) {
		const { jobs } = selected(file);
		for (const job of ['unit', 'race', 'nix', 'release', 'wasm', 'plugins', 'docker']) {
			assert.equal(jobs[job], true, `${file}: ${job}`);
		}
	}
	const browser = selected('libraries/browser-lsp/src/test-change.ts');
	assert.equal(browser.jobs.wasm, true);
	const action = selected('applications/github-action/tests/test-change.ts');
	assert.equal(action.jobs.action, true);
	assert.equal(action.jobs['action-tests'], true);
	assert.equal(action.jobs.unit, false);
	const docs = selected('docs/test-change.md');
	assert.equal(docs.jobs.release, true);
	assert.equal(docs.jobs.unit, false);
	// Uncommitted files must not change a committed CI comparison.
	const head = repo.commit('plugins/sdk/rust/src/test_change.rs', 'Rust SDK');
	appendFileSync(join(repo.cwd, '.mise.toml'), '\n# unrelated local change\n');
	writeFileSync(join(repo.cwd, 'untracked.go'), 'package main\n');
	assert.equal(affectedProjects(base, head, repo.cwd).includes('go:actionlint.kjanat.dev/v2'), false);
});

test('unassigned projects run every check and optional trailers cannot skip required jobs', () => {
	const names = ['unit', 'nix', 'plugins'];
	assert.deepEqual(selectJobs('ci', names, ['custom:new-project']), { unit: true, nix: true, plugins: true });
	assert.deepEqual(selectJobs('ci', names, ['custom:new-project'], { full: false, skip: ['nix'] }), {
		unit: true,
		nix: false,
		plugins: true,
	});
	assert.deepEqual(selectJobs('ci', names, [], { full: true, skip: ['nix'] }), {
		unit: true,
		nix: true,
		plugins: true,
	});
});

test('a failed dependency query selects all checks and reports the failure', t => {
	const repo = repository(t);
	const head = repo.commit('source.go', 'Change source');
	repo.event({ pull_request: { base: { sha: repo.base }, head: { sha: head } } });
	const commands = commandEnvironment(repo.cwd);
	assert.deepEqual(
		plan({ ...repo.environment, ...commands, CI_PROFILE: 'test' }, repo.cwd, () => {
			throw new Error('Broken workspace manifest');
		}),
		{ unit: true, nix: true },
	);
	assert.match(readFileSync(commands.GITHUB_STEP_SUMMARY, 'utf8'), /running every check/);
});

/** @param {import('node:test').TestContext} t */
function repository(t) {
	const cwd = mkdtempSync(join(tmpdir(), 'actionlint-ci-plan-'));
	t.after(() => rmSync(cwd, { recursive: true, force: true }));
	mkdirSync(join(cwd, '.github/ci'), { recursive: true });
	mkdirSync(join(cwd, '.github/workflows'), { recursive: true });
	writeFileSync(
		join(cwd, '.github/workflows/test.yml'),
		`jobs:
  unit:
    if: \u0024{{ fromJSON(needs.changes.outputs.jobs)['unit'] }}
  nix:
    if: \u0024{{ fromJSON(needs.changes.outputs.jobs)['nix'] }}
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
		base: repo.base,
		head,
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
		base: repo.base,
		head,
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
		const { trailers } = changedInputs({ ...repo.environment, GITHUB_EVENT_NAME: event }, repo.cwd);
		assert.deepEqual(selectJobs('test', ['nix', 'unit'], [], trailers), {
			nix: true,
			unit: true,
		});
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

test('documentation workflows install their tools through Runner and mise', () => {
	const root = new URL('../../', import.meta.url);
	const read = (/** @type {string} */ path) => readFileSync(new URL(path, root), 'utf8');
	const pages = read('.github/workflows/pages.yml');
	assert.doesNotMatch(pages, /uses: kjanat\/runner@/);
	assert.match(pages, /MISE_ENABLE_TOOLS: "go,node,npm,bun,pandoc,npm:runner-run"/);
	const upkeep = read('.github/workflows/upkeep.yml');
	assert.match(upkeep, /MISE_ENABLE_TOOLS: "shellcheck"/);
	assert.match(upkeep, /mise lock --bump github:kjanat\/actionlint github:rhysd\/actionlint/);
	assert.match(
		upkeep,
		/MISE_DISABLE_TOOLS: "rshellcheck,github:kjanat\/shellcheck-rs"/,
	);
	assert.match(upkeep, /mise install --locked\s*\n/);
});

test('autofix installs the formatter and the tools required by selected generators', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/autofix.yml', import.meta.url), 'utf8')));
	const job = yamlMapping(yamlMapping(workflow.jobs).autofix);
	assert.ok(Array.isArray(job.steps));
	const steps = job.steps.map(yamlMapping);
	const bootstraps = steps.filter(step => typeof step.uses === 'string' && step.uses.startsWith('jdx/mise-action@'));
	assert.equal(bootstraps.length, 1);
	const [bootstrap] = bootstraps;
	assert.ok(bootstrap);
	assert.equal(bootstrap.if, undefined);
	const options = yamlMapping(bootstrap.with);
	assert.equal(options.install, true);
	assert.equal(options.cache, true);
	assert.equal(options.env, true);
	assert.equal(options.add_shims_to_path, false);
	assert.equal(options.working_directory, undefined);
	const selection = yamlMapping(bootstrap.env).MISE_ENABLE_TOOLS;
	assert.ok(typeof selection === 'string');
	assert.match(selection, /^\$\{\{.*\}\}$/);
	const selectedTools = (/** @type {boolean} */ tables, /** @type {boolean} */ readme) =>
		String(runInNewContext(selection.slice(3, -2), {
			steps: { inputs: { outputs: { tables: String(tables), readme: String(readme) } } },
		})).split(',');
	assert.deepEqual(selectedTools(false, false), ['kprint', 'bun']);
	const allTools = ['kprint', 'node', 'npm', 'bun', 'github:kjanat/actionlint', 'github:rhysd/actionlint'];
	for (const [tables, readme] of [[true, false], [false, true], [true, true]]) {
		assert.deepEqual(selectedTools(tables, readme), allTools);
	}
	assert.equal(
		steps.some(step =>
			typeof step.uses === 'string' && /^(?:kjanat\/install-dprint|actions\/setup-node)@/.test(step.uses)
		),
		false,
	);
	const dependencies = steps.find(step => typeof step.run === 'string' && step.run.startsWith('bun install '));
	assert.ok(dependencies);
	assert.equal(dependencies.if, "steps.inputs.outputs.tables == 'true'");
	const formatter = steps.find(step => step.id === 'format');
	assert.ok(formatter && typeof formatter.run === 'string');
	assert.match(formatter.run, /\bkprint fmt\b/);
	for (const id of ['readme', 'tables', 'format']) {
		assert.ok(steps.findIndex(step => step.id === id) > steps.indexOf(bootstrap), `${id} must follow mise setup`);
	}
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

test('Runner uses the cached npm installation', () => {
	const directory = new URL('../workflows/', import.meta.url);
	for (const name of readdirSync(directory).filter(name => /\.ya?ml$/.test(name))) {
		const source = readFileSync(new URL(name, directory), 'utf8');
		assert.doesNotMatch(source, /uses: kjanat\/runner@/, name);
		const workflow = yamlMapping(YAML.parse(source));
		for (const [jobName, value] of Object.entries(yamlMapping(workflow.jobs))) {
			const job = yamlMapping(value);
			const callsRunner = [...yamlObjects(job)].some(step =>
				typeof step.run === 'string' && /(?:^|[;\n])\s*(?:run|runner)\s/.test(step.run)
			);
			if (!callsRunner) continue;
			const selected = String(yamlMapping(job.env).MISE_ENABLE_TOOLS ?? '').split(',');
			assert.ok(selected.includes('npm:runner-run'), `${name}:${jobName}`);
			assert.ok(selected.includes('bun'), `${name}:${jobName} installs npm tools with Bun`);
		}
	}
});

test('Comment Cop caches Git objects and runs only trusted base-branch code', () => {
	const workflow = yamlMapping(
		YAML.parse(readFileSync(new URL('../workflows/comment-cop.yml', import.meta.url), 'utf8')),
	);
	const job = yamlMapping(yamlMapping(workflow.jobs)['comment-cop']);
	const steps = [...yamlObjects(job.steps)];
	const checkout = steps.find(step => typeof step.uses === 'string' && step.uses.startsWith('actions/checkout@'));
	assert.ok(checkout);
	assert.equal(yamlMapping(checkout.with).ref, '${{ github.event.pull_request.base.sha }}');
	assert.equal(yamlMapping(checkout.with)['persist-credentials'], false);
	const setup = steps.find(step => typeof step.uses === 'string' && step.uses.startsWith('jdx/mise-action@'));
	assert.ok(setup);
	assert.equal(yamlMapping(setup.with).cache_save_post, true);
	assert.equal(yamlMapping(setup.with).install, false);
	assert.equal(yamlMapping(job.concurrency)['cancel-in-progress'], true);
	assert.equal(yamlMapping(workflow.permissions).contents, 'read');
});

test('mise locks the CI tool artifacts and Python dependencies', () => {
	const root = new URL('../../', import.meta.url);
	const config = yamlMapping(Bun.TOML.parse(readFileSync(new URL('.mise.toml', root), 'utf8')));
	const lock = yamlMapping(Bun.TOML.parse(readFileSync(new URL('mise.lock', root), 'utf8')));
	const platforms = ['linux-x64', 'linux-arm64', 'macos-x64', 'macos-arm64', 'windows-x64', 'windows-arm64'];
	assert.deepEqual(yamlMapping(config.settings).lockfile_platforms, platforms);
	const configuredTools = yamlMapping(config.tools);
	const lockedTools = yamlMapping(lock.tools);
	assert.deepEqual(yamlMapping(configuredTools.npm).depends, ['bun']);

	/** @param {string} name */
	function entry(name) {
		const configured = configuredTools[name];
		const request = typeof configured === 'string' ? configured : yamlMapping(configured).version;
		assert.equal(typeof request, 'string', `${name} version request`);
		const entries = lockedTools[name];
		assert.ok(Array.isArray(entries), `${name} lock entries`);
		const match = entries.map(yamlMapping).find(item =>
			Array.isArray(item.specifiers) && item.specifiers.includes(request)
		);
		assert.ok(match, `${name} must lock its configured version request`);
		assert.equal(typeof match.version, 'string', `${name} locked version`);
		return match;
	}

	for (
		const [names, targets] of [
			[['bun', 'node', 'go', 'shellcheck'], platforms],
			[['pandoc', 'tinygo', 'wasi-sdk', 'wasm-tools', 'wit-bindgen', 'uv', 'zizmor'], ['linux-x64']],
		]
	) {
		for (const name of names) {
			const locked = entry(name);
			for (const platform of targets) {
				const artifact = yamlMapping(locked[`platforms.${platform}`]);
				assert.match(String(artifact.url), /^https:\/\//, `${name} ${platform} URL`);
				assert.match(String(artifact.checksum), /^sha256:[0-9a-f]{64}$/, `${name} ${platform} checksum`);
			}
		}
	}
	for (const name of ['kprint', 'npm', 'npm:runner-run']) {
		assert.match(String(entry(name).backend), /^npm:/, `${name} uses the npm backend`);
	}
	const uv = yamlMapping(entry('yamllint').uv);
	assert.ok(typeof uv.path === 'string');
	assert.ok(uv.path.startsWith('.mise/locks/yamllint/'));
	for (const file of ['pyproject.toml', 'uv.lock']) {
		assert.ok(existsSync(new URL(`${uv.path}/${file}`, root)), `${file} must exist alongside the yamllint lock`);
	}
});

test('mise setup is pinned, cached, and used once per job', () => {
	/** @type {Record<string, string>} */
	const expected = {
		'unit-tests': 'node,npm,bun,shellcheck',
		race: 'node,npm,bun,shellcheck',
		wasm: 'go,node,npm,bun,pandoc,npm:runner-run',
		javascript: 'go,node,npm,bun,npm:runner-run',
		'release-snapshot': 'node,npm,bun,pandoc,npm:runner-run',
		lint: 'bun,shellcheck,npm:runner-run',
		plugins: 'bun,tinygo,wasi-sdk,kprint,wasm-tools,wit-bindgen',
		action: 'node,npm,bun,shellcheck,npm:runner-run',
	};
	/** @type {Record<string, string>} */
	const otherJobs = {
		'autofix.yml:autofix': 'kprint,node,npm,bun,github:kjanat/actionlint,github:rhysd/actionlint',
		'ci-changes.yml:changes': 'bun',
		'expression-probe-tests.yml:test': 'node,npm,bun,npm:runner-run',
		'npm-release.yml:build': 'bun',
		'matcher.yml:matcher-test': 'bun,npm:runner-run',
		'upkeep.yml:go-toolchain': 'bun,npm:runner-run',
		'package-previews.yml:publish': 'bun,uv,yamllint,zizmor,npm:runner-run',
		'changelog-release.yml:publish': 'bun,npm:runner-run',
		'pages.yml:build': 'go,node,npm,bun,pandoc,npm:runner-run',
		'release-prepare.yml:prepare': 'node,npm,bun,shellcheck,pandoc,npm:runner-run',
		'upkeep.yml:readme': 'shellcheck',
	};
	const directory = new URL('../workflows/', import.meta.url);
	for (const name of readdirSync(directory).filter(name => /\.ya?ml$/.test(name))) {
		const workflow = yamlMapping(YAML.parse(readFileSync(new URL(name, directory), 'utf8')));
		for (const [jobName, job] of Object.entries(yamlMapping(workflow.jobs))) {
			const setups = [...yamlObjects(job)].filter(step =>
				typeof step.uses === 'string' && step.uses.startsWith('jdx/mise-action@')
			);
			assert.ok(setups.length <= 1, `${name}:${jobName} must use one setup`);
			if ((name === 'ci.yml' && jobName in expected) || `${name}:${jobName}` in otherJobs) {
				assert.equal(setups.length, 1, `${name}:${jobName} must set up mise`);
			}
			for (const step of setups) {
				assert.equal(step.uses, 'jdx/mise-action@b44d12e7d0d8ee7461bb893e1b1993500433185b');
				const options = yamlMapping(step.with);
				assert.equal(options.version, '2026.11.4', `${name}:${jobName}`);
				assert.equal(options.mise_dir, '${{ github.workspace }}-mise', `${name}:${jobName}`);
				assert.equal(options.cache, true);
				assert.equal(options.auto_update, undefined);
				const cacheKey = String(options.cache_key);
				assert.ok(cacheKey.startsWith('{{default}}-${{ github.workflow }}-${{ github.job }}-'));
				assert.ok(cacheKey.includes('hashFiles('));
				assert.ok(cacheKey.includes("'.mise/locks/**'"), `${name}:${jobName} must hash the tool locks`);
				assert.ok(!String(workflow.name).includes(','), `${name} cache key must not contain commas`);
				assert.equal(yamlMapping(workflow.env).MISE_CACHE_DIR, '${{ github.workspace }}-mise/cache');
				assert.equal(yamlMapping(workflow.env).MISE_LOCKED, '1');
				assert.equal(yamlMapping(workflow.env).MISE_TASK_RUN_AUTO_INSTALL, 'false');
				assert.equal(options.install_args, undefined, `${name}:${jobName} must use the locked tool selection`);
				assert.equal(options.add_shims_to_path, false);
				assert.equal(options.monorepo, false, `${name}:${jobName} installs its selected root tools`);
				if (name === 'ci.yml' && ['unit-tests', 'action'].includes(jobName)) {
					const environment = yamlMapping(step.env);
					assert.equal(environment.TEMP, '${{ runner.temp }}');
					assert.equal(environment.TMP, '${{ runner.temp }}');
					const jobEnvironment = yamlMapping(yamlMapping(job).env);
					assert.equal(jobEnvironment.TEMP, undefined);
					assert.equal(jobEnvironment.TMP, undefined);
				}
				if (options.install === false) {
					assert.equal(options.env, false);
					assert.equal(options.working_directory, '${{ runner.temp }}');
				} else {
					const selected = yamlMapping(yamlMapping(job).env).MISE_ENABLE_TOOLS;
					assert.ok(typeof selected === 'string' && selected, `${name}:${jobName} must select tools`);
					if (
						!(name === 'autofix.yml' && jobName === 'autofix') && step.env
						&& 'MISE_ENABLE_TOOLS' in yamlMapping(step.env)
					) {
						assert.equal(yamlMapping(step.env).MISE_ENABLE_TOOLS, selected);
					}
					if (name === 'ci.yml' && jobName in expected) assert.equal(selected, expected[jobName], jobName);
					const key = `${name}:${jobName}`;
					if (key in otherJobs) assert.equal(selected, otherJobs[key], key);
				}
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
	assert.match(npm, /MISE_ENABLE_TOOLS: "bun"/);
	assert.match(npm, /bun install --filter @kjanat\/actionlint-npm --frozen-lockfile --ignore-scripts/);
});

test('expensive CI jobs wait for selected checks while unselected checks can be skipped', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8')));
	const jobs = yamlMapping(workflow.jobs);
	const checks = yamlMapping(jobs.checks);
	assert.deepEqual(checks.needs, ['lint', 'javascript', 'go-guest-modules', 'rust-native']);
	assert.equal(checks.if, '${{ !cancelled() }}');
	const steps = checks.steps;
	assert.ok(Array.isArray(steps));
	assert.equal(yamlMapping(steps[0]).run, 'test "${CHECKS_PASSED}" = true');
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

test('workflow lint with all ShellCheck checks runs before the matrix with both tools installed', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8')));
	const jobs = yamlMapping(workflow.jobs);
	const lint = yamlMapping(jobs.lint);
	assert.ok(Array.isArray(lint.steps));
	const steps = lint.steps.map(step => yamlMapping(step));
	const validation = steps.findIndex(step =>
		step.run === "go run ./cmd/actionlint --tool shellcheck='shellcheck -o all'"
	);
	assert.ok(validation > 0);
	assert.ok(steps.slice(0, validation).some(step => step.run === 'uv tool install ruff==0.17.0'));
	const setup = steps.slice(0, validation).flatMap(step =>
		Array.isArray(step.parallel) ? step.parallel.map(item => yamlMapping(item)) : [step]
	);
	assert.equal(yamlMapping(lint.env).MISE_ENABLE_TOOLS, 'bun,shellcheck,npm:runner-run');
	assert.ok(setup.some(step => typeof step.uses === 'string' && step.uses.startsWith('jdx/mise-action@')));
	const action = yamlMapping(jobs.action);
	assert.ok(Array.isArray(action.steps));
	assert.ok(
		!action.steps.some(step =>
			yamlMapping(step).run === "go run ./cmd/actionlint --tool shellcheck='shellcheck -o all'"
		),
	);
});

test('artifact consumers require a successful producer and stop on cancellation', () => {
	const workflow = yamlMapping(YAML.parse(readFileSync(new URL('../workflows/ci.yml', import.meta.url), 'utf8')));
	const jobs = yamlMapping(workflow.jobs);
	/** @type {Array<[string, boolean, boolean]>} */
	const cases = [
		['success', false, true],
		['failure', false, false],
		['skipped', false, false],
		['cancelled', false, false],
		['success', true, false],
	];
	for (const name of ['action-slim', 'package-previews', 'docker-previews']) {
		const job = yamlMapping(jobs[name]);
		assert.ok(typeof job.needs === 'string');
		assert.ok(typeof job.if === 'string', `${name} needs an explicit status condition`);
		const expression = job.if.replace(/^\$\{\{\s*|\s*\}\}$/g, '');
		for (const [result, cancelledRun, expected] of cases) {
			const enabled = runInNewContext(expression, {
				cancelled: () => cancelledRun,
				success: () => false,
				needs: { [job.needs]: { result } },
				github: {
					repository: 'kjanat/actionlint',
					event_name: 'pull_request',
					event: { pull_request: { head: { repo: { full_name: 'kjanat/actionlint' } } } },
				},
			}, { timeout: 100 });
			assert.equal(enabled, expected, `${name}: producer=${result}, cancelled=${cancelledRun}`);
		}
	}
});

test('preview consumers share the successful CI snapshot and cannot trigger separate builds', () => {
	const read = (/** @type {string} */ name) =>
		yamlMapping(YAML.parse(readFileSync(new URL(`../workflows/${name}.yml`, import.meta.url), 'utf8')));
	const ci = read('ci');
	const jobs = yamlMapping(ci.jobs);
	const producer = yamlMapping(jobs['release-snapshot']);
	assert.ok(Array.isArray(producer.steps));
	const steps = producer.steps.map(step => yamlMapping(step));
	const upload = steps.find(step => typeof step.uses === 'string' && step.uses.startsWith('actions/upload-artifact@'));
	assert.ok(upload);
	const artifact = yamlMapping(upload.with);
	assert.ok(typeof artifact.path === 'string' && artifact.path.includes('dist/metadata.json'));
	assert.equal(steps.filter(step => typeof step.uses === 'string' && step.uses.startsWith('goreleaser/')).length, 1);
	assert.deepEqual(yamlMapping(ci.on).push, { branches: ['master'] });
	assert.deepEqual(ci.permissions, { contents: 'read' });
	for (const name of ['package-previews', 'docker-previews']) {
		const caller = yamlMapping(jobs[name]);
		assert.equal(caller.needs, 'release-snapshot', `${name} must wait for the same producer`);
		assert.equal(caller.uses, `$/.github/workflows/${name}.yml`);
		const workflow = read(name);
		assert.deepEqual(Object.keys(yamlMapping(workflow.on)), ['workflow_call']);
		const publish = yamlMapping(yamlMapping(workflow.jobs).publish);
		assert.ok(Array.isArray(publish.steps));
		const consumer = publish.steps.map(step => yamlMapping(step));
		const download = consumer.find(step =>
			typeof step.uses === 'string' && step.uses.startsWith('actions/download-artifact@')
		);
		assert.ok(download);
		assert.equal(yamlMapping(download.with).name, artifact.name);
		assert.equal(yamlMapping(download.with)['run-id'], undefined, 'Consume only this run, including fork PRs');
		assert.ok(!JSON.stringify(consumer).includes('goreleaser/'));
		assert.ok(!JSON.stringify(consumer).includes('go build'));
	}
	const docker = yamlMapping(jobs['docker-previews']);
	assert.ok(typeof docker.if === 'string');
	assert.match(docker.if, /github.repository == 'kjanat\/actionlint'/);
	assert.match(docker.if, /github.event.pull_request.head.repo.full_name == github.repository/);
	for (
		const [repository, eventName, headRepository, expected] of [
			['kjanat/actionlint', 'pull_request', 'contributor/actionlint', false],
			['kjanat/actionlint', 'pull_request', 'kjanat/actionlint', true],
			['kjanat/actionlint', 'push', undefined, true],
			['contributor/actionlint', 'push', undefined, false],
		]
	) {
		// This condition uses only operators shared by JavaScript and GitHub expressions.
		const enabled = runInNewContext(docker.if, {
			cancelled: () => false,
			needs: { 'release-snapshot': { result: 'success' } },
			github: {
				repository,
				event_name: eventName,
				event: { pull_request: { head: { repo: { full_name: headRepository } } } },
			},
		}, { timeout: 100 });
		assert.equal(enabled, expected, `${repository} ${eventName} ${headRepository}`);
	}
	assert.deepEqual(docker.permissions, { contents: 'read', packages: 'write' });
	assert.deepEqual(yamlMapping(jobs['package-previews']).permissions, { contents: 'read' });
	assert.equal(yamlMapping(jobs['package-previews']).secrets, undefined);
});
