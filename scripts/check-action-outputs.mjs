import assert from 'node:assert/strict';

assert.equal(process.env.EXIT_CODE, '0');
assert.equal(process.env.RESULT, 'success');
assert.equal(process.env.PROBLEM_COUNT, '0');
assert.ok(process.env.OUTPUT, 'Missing Action JSON output');
/** @type {unknown} */
const result = JSON.parse(process.env.OUTPUT);
assert.ok(result !== null && typeof result === 'object' && 'diagnostics' in result);
assert.deepEqual(result.diagnostics, []);
assert.partialDeepStrictEqual(result, {
	schema_version: 1,
	status: 'success',
	completed: true,
	exit_code: 0,
	file_count: 1,
});
