import { expect, test } from 'bun:test';

import { imageReferences } from '#images';

test('extracts image scalars in workflows and composite actions with source ranges', () => {
	const workflow = `jobs:
  scan:
    container: alpine:3
    services:
      db:
        image: postgres:16
    steps:
      - uses: docker://busybox:1
      - uses: actions/checkout@v4
      - run: echo docker://ignored
`;
	expect(imageReferences(workflow, 'workflow')).toEqual([
		{ value: 'alpine:3', start: { row: 3, column: 16 }, end: { row: 3, column: 24 } },
		{ value: 'postgres:16', start: { row: 6, column: 16 }, end: { row: 6, column: 27 } },
		{ value: 'busybox:1', start: { row: 8, column: 15 }, end: { row: 8, column: 33 } },
	]);
	const action = `runs:
  using: composite
  steps:
    - uses: docker://alpine:3
    - uses: owner/action@v1
    - run: echo docker://ignored
`;
	expect(imageReferences(action, 'action')).toEqual([
		{ value: 'alpine:3', start: { row: 4, column: 13 }, end: { row: 4, column: 30 } },
	]);
});

test('handles YAML block scalar chomping and rejects embedded newlines', () => {
	for (const style of ['>', '>-', '|', '|-']) {
		const source = `jobs:\n  test:\n    container: ${style}\n      alpine:3\n`;
		const images = imageReferences(source, 'workflow');
		expect(images).toHaveLength(1);
		expect(images[0]?.value).toBe('alpine:3');
		expect(images[0]?.start).toEqual({ row: 3, column: 16 });
		expect(images[0]?.end).toEqual({ row: 5, column: 1 });
	}
	for (
		const scalar of [
			'|+\n      alpine:3\n\n',
			'|-\n      alpine\n      :3\n',
			'>-\n      alpine\n      :3\n',
			'"alpine:3\\n"',
		]
	) {
		expect(() => imageReferences(`jobs:\n  test:\n    container: ${scalar}`, 'workflow')).toThrow();
	}
});

for (
	const { kind, source, label } of [
		{
			kind: 'workflow',
			source: 'env:\n  IMAGE: &image docker://alpine:3\njobs:\n  scan:\n    steps:\n      - uses: *image\n',
			label: 'jobs.scan.steps[0].uses',
		},
		{
			kind: 'action',
			source: 'description: &image docker://alpine:3\nruns:\n  using: composite\n  steps:\n    - uses: *image\n',
			label: 'runs.steps[0].uses',
		},
		{
			kind: 'action',
			source: 'description: &image docker://alpine:3\nruns:\n  using: docker\n  image: *image\n',
			label: 'runs.image',
		},
	] satisfies { kind: 'workflow' | 'action'; source: string; label: string }[]
) {
	test(`rejects an unresolved alias at ${label}`, () => {
		expect(() => imageReferences(source, kind)).toThrow(
			`${label} uses a YAML alias; image target is unresolved`,
		);
	});
}

test('ignores repository actions and Dockerfile paths', () => {
	expect(
		imageReferences('jobs:\n  scan:\n    steps:\n      - uses: owner/action@v1\n      - uses: ./action\n', 'workflow'),
	)
		.toEqual([]);
	for (const image of ['Dockerfile', './Dockerfile']) {
		expect(imageReferences(`runs:\n  using: docker\n  image: ${image}\n`, 'action')).toEqual([]);
	}
});

test('extracts a Docker action image with its source range', () => {
	expect(imageReferences('runs:\n  using: docker\n  image: docker://alpine:3\n', 'action')).toEqual([
		{ value: 'alpine:3', start: { row: 3, column: 10 }, end: { row: 3, column: 27 } },
	]);
});
