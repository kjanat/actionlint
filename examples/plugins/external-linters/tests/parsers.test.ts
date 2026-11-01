/** biome-ignore-all lint/suspicious/noTemplateCurlyInString: gh-syntax */
import assert from 'node:assert/strict';

import { readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

import { test } from 'bun:test';
import { imageReferences } from '@kjanat-actionlint/plugin-sdk/images';
import { parseSarif } from '@kjanat-actionlint/plugin-sdk/sarif';

import { parseGrantJSON } from '../grant/actionlint.plugin.ts';
import { validateSyftSBOM } from '../syft/actionlint.plugin.ts';
import { parseYamllint } from '../yamllint/actionlint.plugin.ts';
import { parseZizmorSarif } from '../zizmor/actionlint.plugin.ts';

const root = resolve(import.meta.dirname, '..');

test('yamllint parser handles captured 1.38.0 parsable output', async () => {
	const output = await readFile(join(root, 'testdata/yamllint.parsable.txt'), 'utf8');
	const findings = parseYamllint(output, '.github/workflows/test.yml');
	assert.deepEqual(findings.map(({ code, severity, location }) => ({ code, severity, location })), [
		{ code: 'document-start', severity: 'warning', location: { row: 1, column: 1 } },
		{ code: 'truthy', severity: 'warning', location: { row: 2, column: 1 } },
	]);
	assert.throws(() => parseYamllint('unexpected scanner output\n', 'workflow.yml'), /unrecognized output/u);
});

test('zizmor parser maps captured SARIF findings to the supplied document', async () => {
	const output = await readFile(join(root, 'testdata/zizmor.sarif.json'), 'utf8');
	const findings = parseZizmorSarif(output, '.github/workflows/test.yml');
	assert.ok(findings.some((finding) => finding.code === 'zizmor/unpinned-uses'));
	assert.ok(findings.every((finding) => finding.filename === '.github/workflows/test.yml'));
	assert.throws(
		() => parseZizmorSarif(output.replaceAll('<stdin>', 'other.yml'), 'workflow.yml'),
		/outside the captured document/u,
	);
});

test('Runner Guard 3.1.5 SARIF without columns maps to captured workflow lines', async () => {
	const output = await readFile(join(root, 'testdata/runner-guard.sarif.json'), 'utf8');
	const findings = parseSarif(
		output,
		'.github/workflows/test.yml',
		(uri) => uri.toLowerCase().endsWith('\\.github\\workflows\\captured.yml'),
	);
	assert.deepEqual(findings.map(({ code, severity, location }) => ({ code, severity, location })), [
		{ code: 'RGS-001', severity: 'error', location: { row: 9, column: 1 } },
		{ code: 'RGS-002', severity: 'error', location: { row: 12, column: 1 } },
	]);
});

test('image references retain complete YAML scalar spans and reject unresolved YAML', () => {
	const source = [
		'jobs:',
		'  scan:',
		'    container:',
		'      image: "ghcr.io/example/scanner:1.2"',
		'    services:',
		'      database:',
		'        image: postgres:16',
		'    steps:',
		'      - uses: docker://alpine:3.20',
	].join('\n');
	const found = imageReferences(source, 'workflow');
	assert.deepEqual(found.map(({ value, start, end }) => ({ value, start, end })), [
		{ value: 'ghcr.io/example/scanner:1.2', start: { row: 4, column: 14 }, end: { row: 4, column: 43 } },
		{ value: 'postgres:16', start: { row: 7, column: 16 }, end: { row: 7, column: 27 } },
		{ value: 'alpine:3.20', start: { row: 9, column: 15 }, end: { row: 9, column: 35 } },
	]);
	const escaped = 'jobs: { build🍊: { steps: [{ uses: "docker\\u003a//ghcr.io/example/image:1" }] } }';
	assert.deepEqual(imageReferences(escaped, 'workflow'), [
		{
			value: 'ghcr.io/example/image:1',
			start: { row: 1, column: 35 },
			end: { row: 1, column: 74 },
		},
	]);
	const block = 'jobs:\n  scan:\n    container: >\n      ghcr.io/example/image:1\n';
	assert.deepEqual(imageReferences(block, 'workflow'), [
		{
			value: 'ghcr.io/example/image:1',
			start: { row: 3, column: 16 },
			end: { row: 5, column: 1 },
		},
	]);
	assert.throws(
		() => imageReferences('jobs:\n  scan:\n    container: "${{ vars.IMAGE }}"\n', 'workflow'),
		/dynamic image reference/u,
	);
	assert.throws(
		() => imageReferences('base: &image nginx:1\njobs:\n  scan:\n    container: *image\n', 'workflow'),
		/YAML alias/u,
	);
});

test('Syft 1.55.0 emits a structured SBOM artifact', async () => {
	const output = await readFile(join(root, 'testdata/syft.sbom.json'), 'utf8');
	assert.doesNotThrow(() => validateSyftSBOM(output));
	assert.throws(() => validateSyftSBOM('{"artifacts":[]}'), /complete Syft JSON SBOM/u);
});

test('Grant 0.6.8 JSON maps denied package findings to the captured image scalar', async () => {
	const output = await readFile(join(root, 'testdata/grant.json'), 'utf8');
	const imageSource = 'jobs:\n  scan:\n    container: ghcr.io/example/app:1\n';
	const image = imageReferences(imageSource, 'workflow')[0];
	if (image === undefined) throw new Error('fixture did not contain an image');
	assert.deepEqual(parseGrantJSON(output, '.github/workflows/test.yml', image), [
		{
			filename: '.github/workflows/test.yml',
			code: 'grant/license-policy',
			message: 'Grant denied sample-denied-package@1.2.3 in ghcr.io/example/app:1: denied licenses: GPL-3.0-only',
			severity: 'error',
			location: { row: 3, column: 16 },
			end_location: { row: 3, column: 37 },
		},
	]);
	assert.throws(
		() =>
			parseGrantJSON(
				output.replace('"status": "noncompliant"', '"status": "error"'),
				'.github/workflows/test.yml',
				image,
			),
		/could not evaluate/u,
	);
});
