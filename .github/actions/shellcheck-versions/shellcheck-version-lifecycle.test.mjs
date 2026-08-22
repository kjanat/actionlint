import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import {
	checkVersions,
	compareVersions,
	parseVersion,
	planVersionUpdate,
	reconcileTrackingIssue,
	TRACKING_MARKER,
	VERSION_FILES,
	versionVerdict,
} from './shellcheck-version-lifecycle.mjs';

const dependency = 'github.com/wasilibs/go-shellcheck/cmd/shellcheck@';

function versionFiles(version) {
	return new Map(VERSION_FILES.map((file) => [file, `${file}: ${dependency}${version}\n`]));
}

function mockCore() {
	const outputs = new Map();
	const messages = [];
	const summary = {
		addHeading() {
			return this;
		},
		addTable() {
			return this;
		},
		addRaw() {
			return this;
		},
		async write() {
			return this;
		},
	};
	return {
		outputs,
		messages,
		core: {
			setOutput(name, value) {
				outputs.set(name, value);
			},
			info(message) {
				messages.push(message);
			},
			summary,
		},
	};
}

function versionGithub({ shellcheck = 'v0.12.0', goShellcheck = 'v0.12.1', embedded = 'v0.12.0' } = {}) {
	return {
		rest: {
			repos: {
				async getLatestRelease({ repo }) {
					return { data: { tag_name: repo === 'shellcheck' ? shellcheck : goShellcheck } };
				},
				async getContent() {
					return {
						data: { content: Buffer.from(`${embedded}\n`).toString('base64'), encoding: 'base64' },
					};
				},
			},
		},
	};
}

async function temporaryWorkspace(t, version = 'v0.11.1') {
	const workspace = await fs.mkdtemp(path.join(os.tmpdir(), 'actionlint-shellcheck-versions-'));
	t.after(() => fs.rm(workspace, { recursive: true, force: true }));
	for (const [file, contents] of versionFiles(version)) {
		const target = path.join(workspace, file);
		await fs.mkdir(path.dirname(target), { recursive: true });
		await fs.writeFile(target, contents);
	}
	return workspace;
}

test('stable versions are parsed and compared numerically', () => {
	assert.deepEqual(parseVersion('v10.2.30'), [10, 2, 30]);
	assert.equal(compareVersions('v0.12.0', 'v0.11.9'), 1);
	assert.equal(compareVersions('v1.0.0', 'v1.0.0'), 0);
	assert.equal(compareVersions('v1.0.0', 'v2.0.0'), -1);
	for (const invalid of ['latest', '0.11.0', 'v0.11', 'v0.11.0-rc1', 'v01.2.3']) {
		assert.throws(() => parseVersion(invalid), /stable vMAJOR\.MINOR\.PATCH/);
	}
});

test('verdict preserves the independent dependency and wrapper states', () => {
	assert.equal(versionVerdict(false, false), 'current');
	assert.equal(versionVerdict(true, false), 'dependency-update-available');
	assert.equal(versionVerdict(false, true), 'wrapper-update-needed');
	assert.equal(versionVerdict(true, true), 'dependency-update-available-and-wrapper-update-needed');
});

test('pin updates are atomic across all version sources', () => {
	const update = planVersionUpdate(versionFiles('v0.11.1'), 'v0.12.1');
	assert.equal(update.changed, true);
	assert.equal(update.currentVersion, 'v0.11.1');
	for (const contents of update.files.values()) {
		assert.match(contents, /shellcheck@v0\.12\.1/);
		assert.doesNotMatch(contents, /shellcheck@v0\.11\.1/);
	}

	const mismatched = versionFiles('v0.11.1');
	mismatched.set(VERSION_FILES[1], `${dependency}v0.11.2\n`);
	assert.throws(() => planVersionUpdate(mismatched, 'v0.12.1'), /pins disagree/);
	assert.throws(() => planVersionUpdate(versionFiles('v0.12.1'), 'v0.11.1'), /refusing|older than pinned/);
});

test('check-only mode reports an outdated pin without modifying files', async (t) => {
	const workspace = await temporaryWorkspace(t);
	const { core, outputs } = mockCore();
	const status = await checkVersions({
		github: versionGithub(),
		core,
		workspace,
	});

	assert.equal(status.verdict, 'dependency-update-available');
	assert.equal(status.pinnedOutdated, true);
	assert.equal(status.filesUpdated, false);
	assert.equal(outputs.get('pinned-outdated'), 'true');
	for (const file of VERSION_FILES) {
		assert.match(await fs.readFile(path.join(workspace, file), 'utf8'), /shellcheck@v0\.11\.1/);
	}
});

test('update mode rewrites every pin and the release gate rejects an old pin', async (t) => {
	const workspace = await temporaryWorkspace(t);
	const first = mockCore();
	const status = await checkVersions({
		github: versionGithub(),
		core: first.core,
		workspace,
		updateFiles: true,
	});
	assert.equal(status.filesUpdated, true);
	for (const file of VERSION_FILES) {
		assert.match(await fs.readFile(path.join(workspace, file), 'utf8'), /shellcheck@v0\.12\.1/);
	}

	const staleWorkspace = await temporaryWorkspace(t);
	await assert.rejects(
		checkVersions({
			github: versionGithub(),
			core: mockCore().core,
			workspace: staleWorkspace,
			failOnOutdated: true,
		}),
		/pinned go-shellcheck v0\.11\.1 is outdated/,
	);
});

test('tracking issue opens when the wrapper lags', async () => {
	const created = [];
	const github = {
		paginate: async () => [],
		rest: {
			issues: {
				async create(input) {
					created.push(input);
					return { data: { html_url: 'https://github.com/kjanat/actionlint/issues/123' } };
				},
			},
		},
	};
	await reconcileTrackingIssue({
		github,
		context: { repo: { owner: 'kjanat', repo: 'actionlint' } },
		core: mockCore().core,
		env: {
			SHELLCHECK_VERSION: 'v0.12.0',
			GO_SHELLCHECK_VERSION: 'v0.11.1',
			EMBEDDED_SHELLCHECK_VERSION: 'v0.11.0',
			GO_SHELLCHECK_LAGGING: 'true',
			BUMP_PR_URL: '',
		},
	});
	assert.equal(created.length, 1);
	assert.match(created[0].body, new RegExp(TRACKING_MARKER));
	assert.equal(created[0].title, 'Track v0.12.0 support in go-shellcheck');
});

test('tracking issue closes and links the dependency PR when the wrapper catches up', async () => {
	const comments = [];
	const updates = [];
	const issue = {
		number: 123,
		state: 'open',
		title: 'Track v0.12.0 support in go-shellcheck',
		body: TRACKING_MARKER,
		html_url: 'https://github.com/kjanat/actionlint/issues/123',
	};
	const github = {
		paginate: async () => [issue],
		rest: {
			issues: {
				async createComment(input) {
					comments.push(input);
				},
				async update(input) {
					updates.push(input);
				},
			},
		},
	};
	await reconcileTrackingIssue({
		github,
		context: { repo: { owner: 'kjanat', repo: 'actionlint' } },
		core: mockCore().core,
		env: {
			SHELLCHECK_VERSION: 'v0.12.0',
			GO_SHELLCHECK_VERSION: 'v0.12.1',
			EMBEDDED_SHELLCHECK_VERSION: 'v0.12.0',
			GO_SHELLCHECK_LAGGING: 'false',
			BUMP_PR_URL: 'https://github.com/kjanat/actionlint/pull/124',
		},
	});
	assert.match(comments[0].body, /pull\/124/);
	assert.equal(updates[0].state, 'closed');
	assert.equal(updates[0].state_reason, 'completed');
});
