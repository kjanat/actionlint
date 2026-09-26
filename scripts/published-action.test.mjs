import assert from 'node:assert/strict';
import test from 'node:test';

import { publishedAction } from './published-action.mjs';

test('published smoke uses both external pins and downloads their binary', () => {
	const commit = 'a'.repeat(40);
	const action = publishedAction('fixture/actionlint', '1.2.3', commit);
	assert.equal(action.runs.using, 'composite');
	const uses = action.runs.steps.filter((step) => 'uses' in step);
	assert.deepEqual(uses.map((step) => step.uses), ['fixture/actionlint@v1.2.3', `fixture/actionlint@${commit}`]);
	for (const step of uses) assert.equal(step.env.ACTIONLINT_ACTION_BINARY, '');
	const checks = action.runs.steps.filter((step) => 'run' in step);
	assert.equal(checks.length, 2);
	assert.equal(checks[0].env.EXIT_CODE, `\${{ steps.version.outputs.exit-code }}`);
	assert.equal(checks[1].env.EXIT_CODE, `\${{ steps.commit.outputs.exit-code }}`);
	for (const step of checks) {
		assert.equal(step.shell, 'bash');
		assert.match(step.run, /test "\$PROBLEM_COUNT" = 0/);
		assert.match(step.run, /test "\$OUTPUT" = '\[\]'/);
	}
});

test('published smoke rejects arbitrary expressions and malformed references', () => {
	for (const repository of ['owner/repo@main', `\${{ github.repository }}`, 'owner/repo\n']) {
		assert.throws(() => publishedAction(repository, '1.2.3', 'a'.repeat(40)), /repository/);
	}
	for (const version of ['main', 'v1.2.3', '01.2.3', '1.2.3\n']) {
		assert.throws(() => publishedAction('owner/repo', version, 'a'.repeat(40)), /version/);
	}
	for (const commit of ['main', 'a'.repeat(39), `${'a'.repeat(40)}\n`]) {
		assert.throws(() => publishedAction('owner/repo', '1.2.3', commit), /commit/);
	}
});
