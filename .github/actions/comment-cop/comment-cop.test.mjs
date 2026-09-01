import assert from 'node:assert/strict';
import test from 'node:test';

import { groupsFromPatch, keyFor } from './comment-cop.mjs';

test('flags a long implementation comment', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,4 @@
+\t// Parse the value here.
+\t// Keep the original around.
+\t// Return both values.
+\tparse(value)
`,
	);

	assert.deepEqual(groups.map(group => group.reasons), [['3 lines']]);
});

test('does not measure Go doc comments by length', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,4 @@
+// Parser reads workflows.
+// It reports invalid syntax.
+// It returns every diagnostic.
+type Parser struct{}
`,
	);

	assert.deepEqual(groups, []);
});

test('flags style tells at any length', () => {
	const groups = groupsFromPatch(
		'rule.go',
		`\
@@ -0,0 +1,2 @@
+\t// Use the cache rather than parsing twice.
+\treturn cache
`,
	);

	assert.deepEqual(groups.map(group => group.reasons), [['"X rather than Y"']]);
});

test('scans Markdown prose but skips fenced code', () => {
	const groups = groupsFromPatch(
		'docs/checks.md',
		`\
@@ -0,0 +1,7 @@
+That said, this paragraph is prose.
+
+\`\`\`go
+// This comment uses robust machinery.
+\`\`\`
+
+- Moreover, this item is separate.
`,
	);

	assert.deepEqual(
		groups.map(group => group.reasons),
		[['filler phrase'], ['connective glue']],
	);
});

test('restores Markdown fence state before each diff hunk', () => {
	const source = [
		'# Example',
		'',
		'````markdown',
		'This contains a shorter ``` marker.',
		'Still inside the fence.',
		'That said, this is code-fence content.',
		'````',
	].join('\n');
	const patch = [
		'@@ -5,0 +6,1 @@',
		'+That said, this is code-fence content.',
	].join('\n');

	assert.deepEqual(groupsFromPatch('docs/checks.md', patch, source), []);
});

test('ignores unsupported file types', () => {
	const groups = groupsFromPatch(
		'fixture.txt',
		`\
@@ -0,0 +1,3 @@
+// one
+// two
+// three
`,
	);

	assert.deepEqual(groups, []);
});

test('uses opaque location-specific marker keys', () => {
	const groups = groupsFromPatch(
		'docs/design notes.md',
		`\
@@ -0,0 +1,1 @@
+That said, repeated prose.
@@ -9,0 +10,1 @@
+That said, repeated prose.
`,
	);

	const keys = groups.map(keyFor);
	assert.equal(keys.length, 2);
	assert.match(keys[0], /^[a-f0-9]{16}$/);
	assert.match(keys[1], /^[a-f0-9]{16}$/);
	assert.notEqual(keys[0], keys[1]);
});
