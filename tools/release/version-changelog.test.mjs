import assert from 'node:assert/strict';
import test from 'node:test';

import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import {
	changelogReleaseNotes,
	prepareChangelog,
	unreleasedEntries,
	validateChangelog,
} from '#release/version-changelog.mjs';

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

test('historical changelog sections of the repository yield release notes', async () => {
	const changelog = await readFile(resolve(import.meta.dirname, '../../CHANGELOG.md'), 'utf8');
	const notes = changelogReleaseNotes(changelog, 'v1.11.0');
	assert.match(notes, /os\.Root/);
	assert.doesNotMatch(notes, /\[Changes\]|releases\/tag\//);
});
