import assert from 'node:assert/strict';
import { test } from 'node:test';

import { annotation } from '#reporters';

test('annotation advice and documentation use encoded line breaks at every severity', () => {
	for (const severity of ['error', 'warning', 'info'] as const) {
		const message =
			'"github.event.head_commit.message" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://example.com/help?q=10%25 for more details';
		const finding = {
			rule: 'expression',
			path: 'ci.yml',
			severity,
			message,
			start: { line: 6, column: 41 },
			end: { line: 6, column: 73 },
		};
		const text = annotation(finding);
		assert.ok(text.startsWith(`::${severity === 'info' ? 'notice' : severity} `));
		assert.ok(text.includes('untrusted.%0Aavoid'));
		assert.ok(text.includes('scripts.%0Ainstead'));
		assert.ok(text.includes('variable.%0Asee https://example.com/help?q=10%2525'));
		assert.ok(!text.includes('\n'));
		assert.equal(finding.message, message);
	}
});
