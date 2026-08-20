import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import object from './object.mjs';

const pattern = object.problemMatcher[0].pattern[0];
const regexp = new RegExp(pattern.regexp);
const dirname = path.dirname(fileURLToPath(import.meta.url));
const testdata = path.join(dirname, 'testdata');

describe('problem matcher pattern', () => {
	const want = JSON.parse(fs.readFileSync(path.join(testdata, 'want.json'), 'utf8'))[0];

	for (const name of ['escape.txt', 'no_escape.txt']) {
		it(`captures every field of ${name}`, () => {
			const [line] = fs.readFileSync(path.join(testdata, name), 'utf8').split('\n');
			const m = line.match(regexp);
			assert.ok(m, `${line} did not match ${regexp}`);
			assert.equal(m[pattern.file], want.filepath);
			assert.equal(m[pattern.line], want.line.toString());
			assert.equal(m[pattern.column], want.column.toString());
			assert.equal(m[pattern.message], want.message);
			assert.equal(m[pattern.code], want.kind);
		});
	}

	for (const parent of ['examples', 'err']) {
		const dir = path.join(dirname, '..', '..', 'testdata', parent);
		for (const name of fs.readdirSync(dir)) {
			if (!name.endsWith('.out')) {
				continue;
			}
			it(`matches every error of ${parent}/${name}`, () => {
				for (const line of fs.readFileSync(path.join(dir, name), 'utf8').split('\n')) {
					if (line.length === 0 || line.startsWith('/')) {
						continue;
					}
					const msg = `Line '${line}' did not match to the regex ${regexp}`;
					const m = line.match(regexp);
					assert.ok(m, msg);
					assert.equal(m[pattern.file], 'test.yaml', msg);
					assert.match(m[pattern.line], /^\d+$/, msg);
					assert.match(m[pattern.column], /^\d+$/, msg);
					assert.ok(m[pattern.message].length > 0, msg);
					assert.ok(m[pattern.code].length > 0, msg);
				}
			});
		}
	}
});
