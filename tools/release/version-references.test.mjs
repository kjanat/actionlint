import assert from 'node:assert/strict';
import { chmod, cp, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';

import {
	changelogReleaseNotes,
	prepareChangelog,
	unreleasedEntries,
	validateChangelog,
} from '#release/version-changelog.mjs';
import {
	checkReferences,
	compareVersions,
	parseVersion,
	planVersionUpdates,
	rewriteReferences,
	scanReferences,
	writeUpdates,
} from '#release/version-references.mjs';
import { targets } from '#release/version-targets.mjs';

/** @type {import('#release/version-references.mjs').VersionTarget[]} */
const fixtureTargets = [
	{
		path: 'docs.md',
		rules: [{ description: 'package reference', pattern: /`tool@(\d+\.\d+\.\d+)`/dg, count: 1 }, {
			description: 'action reference',
			pattern: /uses: example\/tool@v(\d+\.\d+\.\d+)/dg,
			count: 1,
		}],
		unrelated: ['sarif v2.1.0'],
	},
	{
		path: 'download.bash',
		rules: [{ description: 'default version', pattern: /^version="(\d+\.\d+\.\d+)"\r?$/dgm, count: 1 }],
	},
	{
		path: 'page.html',
		rules: [{ description: 'version badge', pattern: /id="version">v(\d+\.\d+\.\d+)/dg, count: 1 }],
	},
];

/** @param {import('node:test').TestContext} t */
async function fixture(t) {
	const root = await mkdtemp(join(tmpdir(), 'actionlint-version-'));
	t.after(() => rm(root, { recursive: true, force: true }));
	await cp(new URL('./testdata/version/repo/', import.meta.url), root, { recursive: true });
	return root;
}

test('release versions have numeric components without prefixes, suffixes or leading zeroes', () => {
	for (const version of ['1.2.3', '0.0.0', '1.11.0']) {
		assert.equal(parseVersion(version).map(String).join('.'), version);
	}
	for (const version of ['v1.2.3', '01.2.3', '1.02.3', '1.2.03', '1.2', '1.2.3.4', '1.2.3-rc1', '']) {
		assert.throws(() => parseVersion(version), /three dot-separated/);
	}
	assert.equal(compareVersions('1.2.3', '1.2.3'), 0);
	assert.equal(compareVersions('1.2.3', '1.2.4'), -1);
	assert.equal(compareVersions('1.2.4', '1.2.3'), 1);
	assert.equal(compareVersions('1.9.0', '1.10.0'), -1);
	assert.equal(compareVersions('2.0.0', '1.99.99'), 1);
});

test('updates match the existing version fixtures and a repeated update changes nothing', async t => {
	const root = await fixture(t);
	assert.equal(await checkReferences(root, fixtureTargets), '1.2.0');
	await writeUpdates(root, await planVersionUpdates(root, fixtureTargets, '2.0.0'));
	for (const target of fixtureTargets) {
		const content = await readFile(join(root, target.path), 'utf8');
		assert.equal(content, await readFile(new URL(`./testdata/version/want/${target.path}`, import.meta.url), 'utf8'));
		assert.equal(rewriteReferences(target, content, '2.0.0'), content);
	}
	assert.equal(await checkReferences(root, fixtureTargets), '2.0.0');
});

test('missing, extra, undeclared and overlapping references stop preparation', async t => {
	const target = fixtureTargets[0];
	const original = await readFile(new URL('./testdata/version/repo/docs.md', import.meta.url), 'utf8');
	assert.throws(() => scanReferences(target, original.replace('tool@1.2.0', 'tool@latest')), /expected 1 occurrence/);
	assert.throws(() => scanReferences(target, `${original}\n\`tool@1.2.0\``), /found 2/);
	assert.throws(() => scanReferences(target, `${original}\nunknown 1.2.3`), /docs.md:.*undeclared version/);
	assert.throws(
		() => scanReferences(target, original.replace('sarif v2.1.0', 'sarif latest')),
		/unrelated version reference.*absent/,
	);
	assert.throws(() => scanReferences({ ...target, rules: [...target.rules, target.rules[0]] }, original), /overlap/);
	assert.throws(() => rewriteReferences(target, original, '1.1.0'), /newer than/);
	const root = await fixture(t);
	await writeFile(join(root, 'page.html'), 'No version here');
	await assert.rejects(planVersionUpdates(root, fixtureTargets, '2.0.0'), /page.html/);
	assert.equal(await readFile(join(root, 'docs.md'), 'utf8'), original);
	await writeFile(
		join(root, 'page.html'),
		await readFile(new URL('./testdata/version/repo/page.html', import.meta.url)),
	);
	await assert.rejects(planVersionUpdates(root, [...fixtureTargets, { path: 'missing', rules: [] }], '2.0.0'), {
		code: 'ENOENT',
	});
});

test('read-only targets cause completed writes to roll back without temporary files', async t => {
	const root = await fixture(t);
	const updates = await planVersionUpdates(root, fixtureTargets, '2.0.0');
	const locked = join(root, 'page.html');
	await chmod(locked, 0o444);
	try {
		await assert.rejects(writeUpdates(root, updates), /read-only/);
		for (const update of updates) assert.equal(await readFile(join(root, update.path), 'utf8'), update.original);
		assert.ok((await readdir(root)).every(name => !name.includes('.version-')));
	} finally {
		await chmod(locked, 0o600);
	}
});

test('edits made after preparation are preserved and stop the write batch', async t => {
	const root = await fixture(t);
	const updates = await planVersionUpdates(root, fixtureTargets, '2.0.0');
	await writeFile(join(root, 'page.html'), 'User edit');
	await assert.rejects(writeUpdates(root, updates), /changed after version preparation/);
	assert.equal(await readFile(join(root, 'docs.md'), 'utf8'), updates[0].original);
	assert.equal(await readFile(join(root, 'page.html'), 'utf8'), 'User edit');
});

test('nightly pins are reported separately, survive updates and do not select a release version', async t => {
	const root = await fixture(t);
	const nightly = {
		path: 'image.txt',
		rules: [{ description: 'image tag', pattern: /tool:(\d+\.\d+\.\d+|nightly)/dg, count: 1 }],
	};
	await writeFile(join(root, nightly.path), 'image: tool:nightly\n');
	/** @type {string[]} */
	const lines = [];
	assert.equal(await checkReferences(root, [...fixtureTargets, nightly], line => lines.push(line)), '1.2.0');
	assert.ok(lines.includes('image.txt:1: image tag: nightly (development)'));
	assert.ok(lines.includes('version 1.2.0 is referenced 4 time(s)'));
	await writeUpdates(root, await planVersionUpdates(root, [...fixtureTargets, nightly], '2.0.0'));
	assert.equal(await readFile(join(root, nightly.path), 'utf8'), 'image: tool:nightly\n');
	await assert.rejects(checkReferences(root, [nightly]), /No declared file/);
	await writeFile(join(root, 'image.txt'), 'image: tool:1.2.0\n');
	await assert.rejects(checkReferences(root, [...fixtureTargets, nightly]), /disagree/);
});

test('CRLF source is preserved and generated versions are excluded from release rewrites', async t => {
	const root = await fixture(t);
	for (const target of fixtureTargets) {
		const content = (await readFile(join(root, target.path), 'utf8')).replaceAll('\n', '\r\n');
		const updated = rewriteReferences(target, content, '2.0.0');
		assert.ok(updated.includes('\r\n'));
		assert.ok(!updated.replaceAll('\r\n', '').includes('\n'));
	}
	const target = {
		path: 'workflow.yml',
		rules: [{ description: 'release', pattern: /release: (\d+\.\d+\.\d+)/dg, count: 1 }],
		generated: [/dependency: v\d+\.\d+\.\d+/g],
	};
	assert.equal(
		rewriteReferences(target, 'release: 1.2.0\ndependency: v3.0.0\n', '2.0.0'),
		'release: 2.0.0\ndependency: v3.0.0\n',
	);
	assert.throws(() => scanReferences(target, 'release: 1.2.0'), /generated-version pattern/);
});

const changelog = `# Unreleased

- Pending.

<a id="v1.11.0"></a>
# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.11.0) - 2026-08-20

- Release the thing.

[Changes][v1.11.0]

[v1.11.0]: https://github.com/kjanat/actionlint/compare/v1.10.0...v1.11.0
`;

test('changelog release notes prefer a matching version section over Unreleased entries', () => {
	validateChangelog(changelog);
	assert.equal(changelogReleaseNotes(changelog, 'v1.11.0'), '- Release the thing.\n');
	assert.equal(changelogReleaseNotes(changelog, 'v1.12.0'), '- Pending.\n');
	assert.deepEqual(unreleasedEntries('# Unreleased\n-\n-   \n- Real entry.\n# Older\n- Other.'), ['- Real entry.']);
	assert.throws(() => unreleasedEntries('# Another heading'), /no "# Unreleased"/);
});

test('changelog validation rejects broken links, duplicate versions and empty release sections', () => {
	for (
		const dropped of [
			'# [v1.11.0](https://github.com/kjanat/actionlint/releases/tag/v1.11.0) - 2026-08-20',
			'[Changes][v1.11.0]',
			'[v1.11.0]: https://github.com/kjanat/actionlint/compare/v1.10.0...v1.11.0',
			'- Release the thing.',
		]
	) {
		assert.throws(() => validateChangelog(changelog.replace(dropped, '')), /v1\.11\.0/);
	}
	assert.throws(() => validateChangelog(`${changelog}\n<a id="v1.11.0"></a>`), /twice/);
	assert.throws(() => validateChangelog(`${changelog}\n[v1.11.0]: https://example.com/v1.11.0`), /twice/);
	assert.throws(
		() => validateChangelog(`${changelog}\n[v1.12.0]: https://example.com/v1.12.0`),
		/without a version section/,
	);
	assert.throws(
		() => validateChangelog(changelog.replace('compare/v1.10.0...v1.11.0', 'compare/v1.10.0...v1.12.0')),
		/ends at another version/,
	);
	assert.throws(
		() => changelogReleaseNotes(changelog.replace('- Pending.', ''), 'v1.12.0'),
		/no v1.12.0 section or Unreleased entries/,
	);
});

test('preparing a changelog moves pending content, defines the comparison and is idempotent', () => {
	const updated = prepareChangelog(changelog, 'v1.12.0', '2026-08-21');
	assert.equal(changelogReleaseNotes(updated, 'v1.12.0'), '- Pending.\n');
	assert.deepEqual(unreleasedEntries(updated), []);
	assert.match(updated, /\[v1\.12\.0\]: https:\/\/github.com\/kjanat\/actionlint\/compare\/v1\.11\.0\.\.\.v1\.12\.0/);
	assert.equal(prepareChangelog(updated, 'v1.12.0', '2026-08-22'), updated);
	assert.throws(
		() => prepareChangelog(changelog.replace('- Pending.', ''), 'v1.12.0', '2026-08-21'),
		/no Unreleased entries/,
	);
	const first = prepareChangelog('# Unreleased\n\n- First release.\n', 'v1.0.0', '2026-08-21');
	assert.match(first, /\[v1\.0\.0\]: https:\/\/github.com\/kjanat\/actionlint\/tree\/v1\.0\.0/);
	validateChangelog(first);
});

test('declared references and historical changelog sections match the repository', async () => {
	const root = resolve(import.meta.dirname, '../..');
	assert.match(await checkReferences(root, targets), /^\d+\.\d+\.\d+$/);
	const notes = changelogReleaseNotes(await readFile(join(root, 'CHANGELOG.md'), 'utf8'), 'v1.11.0');
	assert.match(notes, /os\.Root/);
	assert.doesNotMatch(notes, /\[Changes\]|releases\/tag\//);
});
