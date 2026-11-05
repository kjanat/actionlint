import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import test from 'node:test';

import { findNix, wslDistributions } from '#release/nix-command.mjs';
import { checkRepository, commandExecutor, prepareVersion } from '#release/prepare-version.mjs';
import { checkReferences } from '#release/version-references.mjs';
import { targets } from '#release/version-targets.mjs';

/** @param {import('node:test').TestContext} t */
async function fixture(t) {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-prepare-version-'));
	t.after(() => rm(root, { recursive: true, force: true }));
	for (const target of targets) {
		const path = join(root, target.path);
		await mkdir(dirname(path), { recursive: true });
		await writeFile(path, await readFile(new URL(`../../${target.path}`, import.meta.url)));
	}
	await mkdir(join(root, 'docs/releases'), { recursive: true });
	const current = await checkReferences(root, targets);
	for (const version of [current, '9.9.9']) {
		await writeFile(join(root, `docs/releases/v${version}.md`), 'Release notes.\n');
	}
	await writeFile(
		join(root, 'CHANGELOG.md'),
		`# Unreleased

- Pending change.

<a id="v1.0.0"></a>
# [v1.0.0](https://github.com/kjanat/actionlint/releases/tag/v1.0.0) - 2026-01-01

- First release.

[Changes][v1.0.0]

[v1.0.0]: https://github.com/kjanat/actionlint/tree/v1.0.0
`,
	);
	return root;
}

/** @param {string} root @param {{branch?: string, dirty?: boolean, localTag?: boolean, remoteTag?: boolean, nixFailure?: boolean, dispatchFailure?: boolean}} [options] */
function commands(root, options = {}) {
	/** @type {string[][]} */
	const calls = [];
	/** @type {import('#release/nix-command.mjs').ExecuteCommand} */
	const execute = async (program, args) => {
		calls.push([program, ...args]);
		if (program === 'git') {
			if (args[0] === 'status') return Buffer.from(options.dirty ? ' M unrelated.txt\n' : '');
			if (args[0] === 'rev-parse' && args[1] === '--show-toplevel') return Buffer.from(root);
			if (args[0] === 'rev-parse' && args[1] === '--abbrev-ref') return Buffer.from(options.branch ?? 'master');
			if (args[0] === 'rev-parse' && args[1] === 'HEAD') return Buffer.from('a'.repeat(40));
			if (args[0] === 'tag') {
				assert.equal(args[1], '--list');
				return Buffer.from(options.localTag ? 'v9.9.9' : '');
			}
			if (args[0] === 'ls-remote') return Buffer.from(options.remoteTag ? `${'a'.repeat(40)}\trefs/tags/v9.9.9` : '');
		} else if (program === 'nix') {
			if (args[0] === 'flake' && options.nixFailure) throw new Error('Nix rejected the candidate');
		} else if (program === 'gh') {
			if (options.dispatchFailure) throw new Error('Dispatch unavailable');
		} else {
			throw new Error(`Unexpected program ${program}`);
		}
		return Buffer.alloc(0);
	};
	return { calls, execute };
}

/** @param {string} root @param {Partial<import('#release/prepare-version.mjs').VersionOptions>} [overrides] */
function options(root, overrides = {}) {
	return { root, version: '9.9.9', check: false, commit: false, push: false, nixArguments: [], ...overrides };
}

test('check mode reads declarations and notes without running Git, Nix or publication commands', async t => {
	const root = await fixture(t);
	const runner = commands(root);
	const before = await readFile(join(root, 'CHANGELOG.md'), 'utf8');
	/** @type {string[]} */
	const lines = [];
	const result = await prepareVersion(options(root, { check: true, version: undefined }), {
		...runner,
		report: line => lines.push(line),
	});
	assert.deepEqual(runner.calls, []);
	assert.ok(lines.includes(`docs/releases/v${result.version}.md describes v${result.version}`));
	assert.equal(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), before);
	await assert.rejects(prepareVersion(options(root, { check: true }), runner), /--check does not accept/);
});

test('commit-only preparation does not push, and a failed dispatch reports the already-pushed source', async t => {
	const local = await fixture(t);
	const localRunner = commands(local);
	await prepareVersion(options(local, { commit: true }), { ...localRunner, report: () => {} });
	assert.ok(localRunner.calls.some(call => call[1] === 'commit'));
	assert.ok(!localRunner.calls.some(call => call[1] === 'push' || call[0] === 'gh'));
	const dispatched = await fixture(t);
	const dispatchRunner = commands(dispatched, { dispatchFailure: true });
	await assert.rejects(
		prepareVersion(options(dispatched, { push: true }), { ...dispatchRunner, report: () => {} }),
		new RegExp(`source=${'a'.repeat(40)}`),
	);
	assert.ok(dispatchRunner.calls.some(call => call[1] === 'push'));
});

test('missing release notes and unavailable Nix stop preparation before any version edits', async t => {
	for (const failure of ['notes', 'nix']) {
		const root = await fixture(t);
		const before = await checkReferences(root, targets);
		const changelog = await readFile(join(root, 'CHANGELOG.md'), 'utf8');
		const runner = commands(root);
		if (failure === 'notes') await rm(join(root, 'docs/releases/v9.9.9.md'));
		/** @type {import('./nix-command.mjs').ExecuteCommand} */
		const execute = async (program, args, commandOptions) => {
			if (program === 'nix') throw new Error('Not installed');
			return runner.execute(program, args, commandOptions);
		};
		await assert.rejects(
			prepareVersion(options(root), { execute, platform: 'linux', report: () => {} }),
			failure === 'notes' ? /is missing/ : /requires Nix/,
		);
		assert.equal(await checkReferences(root, targets), before);
		assert.equal(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), changelog);
	}
});

test('dirty local preparation edits only its declared files and does not commit', async t => {
	const root = await fixture(t);
	await writeFile(join(root, 'unrelated.txt'), 'User work');
	const runner = commands(root, { dirty: true, branch: 'work' });
	await prepareVersion(options(root), { ...runner, report: () => {}, now: new Date('2026-08-20T22:30:00Z') });
	assert.equal(await checkReferences(root, targets), '9.9.9');
	assert.equal(await readFile(join(root, 'unrelated.txt'), 'utf8'), 'User work');
	assert.match(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), /v9\.9\.9\) - 2026-08-21/);
	assert.ok(!runner.calls.some(call => call[1] === 'commit' || call[1] === 'push' || call[0] === 'gh'));
});

test('Nix failure stops before staging or committing and leaves reviewable edits', async t => {
	const root = await fixture(t);
	const runner = commands(root, { nixFailure: true });
	await assert.rejects(
		prepareVersion(options(root, { commit: true }), { ...runner, report: () => {} }),
		/Nix verification failed/,
	);
	assert.equal(await checkReferences(root, targets), '9.9.9');
	assert.ok(!runner.calls.some(call => ['add', 'commit', 'push'].includes(call[1]) || call[0] === 'gh'));
});

test('source commits follow Nix verification and publishing dispatches the exact source without creating a tag', async t => {
	const root = await fixture(t);
	const runner = commands(root);
	await prepareVersion(options(root, { push: true }), { ...runner, report: () => {} });
	const operations = runner.calls.map(call => `${call[0]} ${call[1]}`);
	assert.ok(operations.indexOf('nix flake') < operations.indexOf('git add'));
	assert.ok(operations.indexOf('git add') < operations.indexOf('git commit'));
	assert.ok(operations.indexOf('git commit') < operations.indexOf('git push'));
	assert.ok(operations.indexOf('git push') < operations.indexOf('gh workflow'));
	assert.deepEqual(runner.calls.find(call => call[0] === 'gh'), [
		'gh',
		'workflow',
		'run',
		'release-prepare.yml',
		'--ref',
		'master',
		'-f',
		'version=9.9.9',
		'-f',
		`source=${'a'.repeat(40)}`,
	]);
	assert.ok(runner.calls.filter(call => call[1] === 'tag').every(call => call[2] === '--list'));
});

test('repository preflight rejects dirty commits, non-master pushes, detached HEAD and existing tags', async t => {
	const root = await fixture(t);
	for (
		const state of [{ dirty: true }, { branch: 'work' }, { branch: 'HEAD' }, { localTag: true }, { remoteTag: true }]
	) {
		const runner = commands(root, state);
		await assert.rejects(checkRepository(root, 'v9.9.9', true, true, runner.execute));
		assert.ok(!runner.calls.some(call => ['add', 'commit', 'push'].includes(call[1]) || call[0] === 'gh'));
	}
});

test('version preparation commits and pushes to a local bare repository, then dispatches that actual commit', async t => {
	const root = await fixture(t);
	const remote = await mkdtemp(join(tmpdir(), 'actionlint-version-remote-'));
	t.after(() => rm(remote, { recursive: true, force: true }));
	const gitConfig = join(root, 'isolated.gitconfig');
	await writeFile(gitConfig, '');
	const env = {
		...Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_'))),
		GIT_CONFIG_GLOBAL: gitConfig,
		GIT_CONFIG_NOSYSTEM: '1',
		GIT_TERMINAL_PROMPT: '0',
	};
	const actual = commandExecutor(root, new AbortController().signal, env);
	/** @param {...string} args */
	const git = async (...args) => (await actual('git', args)).toString('utf8').trim();
	await git('init', '--initial-branch=master');
	await git('config', 'user.name', 'Release test');
	await git('config', 'user.email', 'release-test@example.invalid');
	await git('add', '.');
	await git('commit', '-m', 'Initial fixture');
	await git('init', '--bare', remote);
	await git('remote', 'add', 'origin', remote);
	const before = await git('rev-parse', 'HEAD');
	/** @type {string[][]} */
	const dispatches = [];
	/** @type {import('#release/nix-command.mjs').ExecuteCommand} */
	const execute = async (program, args) => {
		if (program === 'nix') return Buffer.alloc(0);
		if (program === 'gh') {
			dispatches.push(args);
			return Buffer.alloc(0);
		}
		assert.equal(program, 'git');
		return actual(program, args);
	};
	await prepareVersion(options(root, { push: true }), { execute, report: () => {} });
	const head = await git('rev-parse', 'HEAD');
	assert.notEqual(head, before);
	assert.equal(await git('status', '--porcelain'), '');
	assert.match(await git('show', 'HEAD:flake.nix'), /version = "9\.9\.9"/);
	assert.equal((await git('ls-remote', 'origin', 'refs/heads/master')).split('\t')[0], head);
	assert.equal(await git('tag', '--list'), '');
	assert.deepEqual(dispatches, [[
		'workflow',
		'run',
		'release-prepare.yml',
		'--ref',
		'master',
		'-f',
		'version=9.9.9',
		'-f',
		`source=${head}`,
	]]);
	await mkdir(join(root, 'nested'));
	await assert.rejects(checkRepository(join(root, 'nested'), 'v9.9.10', false, false, execute), /repository root/);
	await git('switch', '-c', 'work');
	await assert.rejects(checkRepository(root, 'v9.9.10', true, true, execute), /requires master/);
});

test('WSL distribution names support UTF-8, UTF-16, BOMs and non-ASCII characters', () => {
	const text = 'Ubuntu\r\nCustom Nix 🐧\r\n';
	for (
		const bytes of [
			Buffer.from(text),
			Buffer.from(`\ufeff${text}`),
			Buffer.from(text, 'utf16le'),
			Buffer.from(`\ufeff${text}`, 'utf16le'),
		]
	) {
		assert.deepEqual(wslDistributions(bytes), ['Ubuntu', 'Custom Nix 🐧']);
	}
	assert.throws(() => wslDistributions(Buffer.from([78, 0, 105])), /Incomplete UTF-16/);
});

test('Nix selection prefers a native command and passes explicit launcher arguments unchanged', async () => {
	/** @type {string[][]} */
	const calls = [];
	/** @type {import('#release/nix-command.mjs').ExecuteCommand} */
	const execute = async (program, args) => {
		calls.push([program, ...args]);
		return Buffer.alloc(0);
	};
	assert.deepEqual(await findNix({ platform: 'win32', args: [] }, execute), { program: 'nix', args: [] });
	assert.deepEqual(
		await findNix({ platform: 'win32', program: 'my launcher', args: ['--distribution', 'Custom Nix'] }, execute),
		{ program: 'my launcher', args: ['--distribution', 'Custom Nix'] },
	);
	assert.deepEqual(calls, [['nix', '--version'], ['my launcher', '--distribution', 'Custom Nix', '--version']]);
	await assert.rejects(findNix({ platform: 'win32', args: ['--bad'] }, execute), /requires --nix-program/);
});

test('Windows Nix detection tries WSL distributions in order, skips failures and stops on cancellation', async () => {
	/** @type {string[][]} */
	const calls = [];
	/** @type {import('#release/nix-command.mjs').ExecuteCommand} */
	const execute = async (program, args) => {
		calls.push([program, ...args]);
		if (args[0] === '--list') return Buffer.from('Ubuntu\nCustom Nix\nUnused\n', 'utf16le');
		if (args[1] === 'Custom Nix') return Buffer.alloc(0);
		throw new Error('Not installed');
	};
	const selected = await findNix({ platform: 'win32', args: [] }, execute);
	assert.equal(selected.program, 'wsl.exe');
	assert.deepEqual(selected.args, ['--distribution', 'Custom Nix', '--exec', 'sh', '-lc', 'exec nix "$@"', 'nix']);
	assert.ok(!calls.some(call => call.includes('Unused')));
	await assert.rejects(findNix({ platform: 'linux', args: [] }, execute), /requires Nix/);
	await assert.rejects(
		findNix({ platform: 'win32', args: [] }, async () => {
			throw new DOMException('Stopped', 'AbortError');
		}),
		{ name: 'AbortError' },
	);
});

test('command execution captures bytes and cancellation terminates the child', async () => {
	const controller = new AbortController();
	const execute = commandExecutor(resolve(import.meta.dirname), controller.signal);
	assert.equal((await execute(process.execPath, ['-e', 'process.stdout.write("ok")'])).toString(), 'ok');
	const running = execute(process.execPath, ['-e', 'setInterval(() => {}, 1000)']);
	controller.abort();
	await assert.rejects(running, { name: 'AbortError' });
});

test('live command output goes to its parent without collecting another copy', async () => {
	const execute = commandExecutor(resolve(import.meta.dirname), new AbortController().signal);
	const program = `
import { commandExecutor } from ${JSON.stringify(new URL('./prepare-version.mjs', import.meta.url).href)};
const result = await commandExecutor(process.cwd(), new AbortController().signal)(
  process.execPath, ['-e', ${JSON.stringify('process.stdout.write("from child\\n")')}], { live: true }
);
if (result.length) throw new Error('Live output was captured instead of inherited');
`;
	assert.equal((await execute(process.execPath, ['--input-type=module', '-e', program])).toString(), 'from child\n');
});
