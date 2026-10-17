import assert from 'node:assert/strict';
import test from 'node:test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';

import { changedInputs, ciTrailers, matchesPaths, plan, selectJobs } from './plan.mjs';

/** @param {import('node:test').TestContext} t */
function repository(t) {
	const cwd = mkdtempSync(join(tmpdir(), 'actionlint-ci-plan-'));
	t.after(() => rmSync(cwd, { recursive: true, force: true }));
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
	assert.equal(matchesPaths('src/index.js', ['**.js']), true);
	assert.equal(matchesPaths('README.md', ['**/README.md']), true);
	assert.equal(matchesPaths('docs/README.md', ['**/README.md']), true);
	assert.equal(matchesPaths('.github/workflows/ci.yml', ['.github/workflows/**']), true);
	assert.throws(() => matchesPaths('page.js', ['*.jsx?']), /Unsupported/);
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
	const head = repo.commit('internal/cli/check_test.go', 'Latest PR change\n\nCI-Skip: nix');
	repo.git(['checkout', '--quiet', 'base']);
	const base = repo.commit('unrelated.go', 'Base advanced');
	repo.git(['merge', '--quiet', '--no-ff', 'pr', '-m', 'Synthetic merge\n\nCI: full']);
	repo.event({ pull_request: { base: { sha: base }, head: { sha: head } } });
	assert.deepEqual(changedInputs(repo.environment, repo.cwd), {
		paths: ['internal/cli/check.go', 'internal/cli/check_test.go'],
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
		CI_FILTERS: JSON.stringify({ shared: [], jobs: { unit: ['**/*.go'], nix: ['nix/**'] } }),
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
					CI_FILTERS: JSON.stringify({ shared: [], jobs: { unit: ['**/*.go'] } }),
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
	const scalar = text.match(/ {6}filters: \|\n((?: {8}.*\n)+)/)?.[1];
	assert.ok(scalar, `${workflow} must declare its check inputs`);
	return JSON.parse(scalar);
}

test('test-only CLI changes keep Go coverage without rebuilding packages or running unrelated suites', () => {
	const jobs = selectJobs(workflowFilters('ci'), ['internal/cli/usability_test.go']);
	assert.deepEqual(Object.keys(jobs).filter(job => jobs[job]), ['unit', 'race', 'lint']);
	for (const workflow of ['codeql', 'conformance', 'expression-probe-tests', 'matcher']) {
		assert.ok(
			Object.values(selectJobs(workflowFilters(workflow), ['internal/cli/usability_test.go'])).every(run => !run),
		);
	}
});

test('action tests, probe results, mixed documentation and source changes select their owners', () => {
	const ci = workflowFilters('ci');
	const action = selectJobs(ci, ['packages/github-action/tests/reporters.test.ts']);
	assert.deepEqual(Object.keys(action).filter(job => action[job]), ['javascript', 'action', 'action-tests']);
	const documentation = selectJobs(ci, ['docs/usage.md']);
	assert.deepEqual(Object.keys(documentation).filter(job => documentation[job]), ['release']);
	assert.equal(selectJobs(ci, ['docs/usage.md', 'config.go']).unit, true);
	assert.equal(
		selectJobs(workflowFilters('expression-probe-tests'), [
			'scripts/expression-probe/results/run.json',
		])['expression-probe'],
		false,
	);
	assert.equal(selectJobs(workflowFilters('conformance'), ['internal/cli/check.go']).conformance, false);
	assert.equal(selectJobs(workflowFilters('conformance'), ['rule_expression.go']).conformance, true);
	for (
		const file of [
			'internal/conformance/conformance_test.go',
			'internal/conformance/conformance_adapters_test.go',
			'internal/conformance/conformance_score_test.go',
			'internal/conformance/conformance_assessments_test.go',
			'internal/conformance/testdata/assessments.json',
			'internal/conformance/testdata/differences.json',
		]
	) {
		assert.equal(selectJobs(workflowFilters('conformance'), [file]).conformance, true, file);
	}
	assert.equal(selectJobs(workflowFilters('matcher'), ['internal/cli/check.go']).matcher, true);
	assert.equal(selectJobs(ci, ['internal/cli/check.go'])['action-tests'], false);
	assert.equal(selectJobs(ci, ['Dockerfile'])['action-tests'], false);
	assert.equal(selectJobs(ci, ['.github/workflows/upkeep.yml']).unit, true);
	assert.ok(Object.values(selectJobs(ci, ['.github/workflows/ci.yml'])).every(Boolean));
});

test('Action smoke fixtures are re-included after the general testdata exclusion', () => {
	const ci = workflowFilters('ci');
	for (const path of ['testdata/ok/minimal.yaml', 'testdata/err/one_error.yaml']) {
		assert.equal(selectJobs(ci, [path]).action, true, path);
	}
	assert.equal(selectJobs(ci, ['testdata/ok/other.yaml']).action, false);
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
