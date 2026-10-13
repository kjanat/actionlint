import assert from 'node:assert/strict';
import test from 'node:test';

import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

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

const dependency = 'github.com/kjanat/shellcheck-rs/go/cmd/shellcheck@';

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

function versionGithub({ shellcheck = 'v0.12.0', rshellcheck = 'v0.12.0', installerTag = true } = {}) {
	const refs = [];
	return {
		refs,
		rest: {
			repos: {
				async getLatestRelease({ repo }) {
					return { data: { tag_name: repo === 'shellcheck' ? shellcheck : `rshellcheck-${rshellcheck}` } };
				},
			},
			git: {
				async getRef({ owner, repo, ref }) {
					refs.push(`${owner}/${repo}@${ref}`);
					if (!installerTag) {
						throw new Error('Not Found');
					}
					return { data: { ref: `refs/${ref}` } };
				},
			},
		},
	};
}

async function temporaryWorkspace(t, version = 'v0.11.0') {
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

test('verdict preserves the independent dependency and port states', () => {
	assert.equal(versionVerdict(false, false), 'current');
	assert.equal(versionVerdict(true, false), 'dependency-update-available');
	assert.equal(versionVerdict(false, true), 'port-update-needed');
	assert.equal(versionVerdict(true, true), 'dependency-update-available-and-port-update-needed');
});

test('pin updates are atomic across all version sources', () => {
	const update = planVersionUpdate(versionFiles('v0.11.0'), 'v0.12.1');
	assert.equal(update.changed, true);
	assert.equal(update.currentVersion, 'v0.11.0');
	for (const contents of update.files.values()) {
		assert.match(contents, /shellcheck@v0\.12\.1/);
		assert.doesNotMatch(contents, /shellcheck@v0\.11\.0/);
	}

	const mismatched = versionFiles('v0.11.0');
	mismatched.set(VERSION_FILES[1], `${dependency}v0.11.2\n`);
	assert.throws(() => planVersionUpdate(mismatched, 'v0.12.1'), /pins disagree/);
	assert.throws(() => planVersionUpdate(versionFiles('v0.12.1'), 'v0.11.0'), /older than pinned/);
	const foreign = versionFiles('v0.11.0');
	foreign.set(VERSION_FILES[0], 'github.com/wasilibs/go-shellcheck/cmd/shellcheck@v0.11.1\n');
	assert.throws(() => planVersionUpdate(foreign, 'v0.12.1'), /expected one pinned rshellcheck installer/);

	const wrapper = planVersionUpdate(versionFiles('v0.11.0-wrapper.1'), 'v0.11.0');
	assert.equal(wrapper.changed, false);
	assert.equal(wrapper.currentVersion, 'v0.11.0-wrapper.1');
	const bumped = planVersionUpdate(versionFiles('v0.11.0-wrapper.1'), 'v0.12.0');
	assert.equal(bumped.changed, true);
	for (const contents of bumped.files.values()) {
		assert.match(contents, /shellcheck@v0\.12\.0\n/);
	}
});

test('check-only mode reports an outdated pin without modifying files', async (t) => {
	const workspace = await temporaryWorkspace(t);
	const { core, outputs } = mockCore();
	const github = versionGithub();
	const status = await checkVersions({ github, core, workspace });

	assert.equal(status.verdict, 'dependency-update-available');
	assert.equal(status.pinnedOutdated, true);
	assert.equal(status.portLagging, false);
	assert.equal(status.filesUpdated, false);
	assert.equal(outputs.get('pinned-outdated'), 'true');
	assert.equal(outputs.get('rshellcheck-version'), 'v0.12.0');
	assert.equal(outputs.get('pinned-rshellcheck-version'), 'v0.11.0');
	assert.deepEqual(github.refs, ['kjanat/shellcheck-rs@tags/go/v0.12.0']);
	for (const file of VERSION_FILES) {
		assert.match(await fs.readFile(path.join(workspace, file), 'utf8'), /shellcheck@v0\.11\.0/);
	}
});

test('a lagging port is reported without a pin update', async (t) => {
	const workspace = await temporaryWorkspace(t);
	const status = await checkVersions({
		github: versionGithub({ shellcheck: 'v0.12.0', rshellcheck: 'v0.11.0' }),
		core: mockCore().core,
		workspace,
	});
	assert.equal(status.verdict, 'port-update-needed');
	assert.equal(status.pinnedOutdated, false);
	assert.equal(status.portLagging, true);
});

test('a release without its installer tag cannot be pinned', async (t) => {
	const workspace = await temporaryWorkspace(t);
	await assert.rejects(
		checkVersions({ github: versionGithub({ installerTag: false }), core: mockCore().core, workspace }),
		/release v0\.12\.0 has no go\/v0\.12\.0 tag/,
	);
	await assert.rejects(
		checkVersions({
			github: versionGithub({ shellcheck: 'v0.11.0', rshellcheck: 'v0.12.0' }),
			core: mockCore().core,
			workspace,
		}),
		/rshellcheck v0\.12\.0 is newer than the latest ShellCheck release v0\.11\.0/,
	);
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
		assert.match(await fs.readFile(path.join(workspace, file), 'utf8'), /shellcheck@v0\.12\.0/);
	}

	const staleWorkspace = await temporaryWorkspace(t);
	await assert.rejects(
		checkVersions({
			github: versionGithub(),
			core: mockCore().core,
			workspace: staleWorkspace,
			failOnOutdated: true,
		}),
		/pinned rshellcheck v0\.11\.0 is outdated/,
	);
});

test('tracking issue opens when the port lags', async () => {
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
			RSHELLCHECK_VERSION: 'v0.11.0',
			RSHELLCHECK_LAGGING: 'true',
			BUMP_PR_URL: '',
		},
	});
	assert.equal(created.length, 1);
	assert.match(created[0].body, new RegExp(TRACKING_MARKER));
	assert.match(created[0].body, /releases\/tag\/rshellcheck-v0\.11\.0/);
	assert.equal(created[0].title, 'Track v0.12.0 support in shellcheck-rs');
});

test('tracking issue closes and links the dependency PR when the port catches up', async () => {
	const comments = [];
	const updates = [];
	const issue = {
		number: 123,
		state: 'open',
		title: 'Track v0.12.0 support in shellcheck-rs',
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
			RSHELLCHECK_VERSION: 'v0.12.0',
			RSHELLCHECK_LAGGING: 'false',
			BUMP_PR_URL: 'https://github.com/kjanat/actionlint/pull/124',
		},
	});
	assert.match(comments[0].body, /pull\/124/);
	assert.equal(updates[0].state, 'closed');
	assert.equal(updates[0].state_reason, 'completed');
});
