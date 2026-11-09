import assert from 'node:assert/strict';
import test from 'node:test';

import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

import { goMetadata, renderGoMetadata, writeGoImportPage } from '#go-metadata';

await test('versioned Go import page and root use shared module metadata', async () => {
	const temporary = await mkdtemp(join(tmpdir(), 'actionlint-go-metadata-'));
	try {
		await writeFile(join(temporary, 'go.mod'), 'module example.org/project/v12\n\ngo 1.27\n');
		const metadata = goMetadata(temporary);
		assert.equal(metadata.root, 'example.org/project');
		assert.equal(metadata.modulePath, 'example.org/project/v12');
		const root = renderGoMetadata('%GO_IMPORT_METADATA%', metadata.root, metadata.repository);
		assert.ok(root.includes(`content="example.org/project git ${metadata.repository}"`));
		await writeGoImportPage(temporary, metadata);
		const page = await readFile(join(temporary, 'v12', 'index.html'), 'utf8');
		assert.ok(page.includes(`content="example.org/project/v12 git ${metadata.repository}"`));
		assert.ok(
			page.includes(`content="example.org/project/v12 ${metadata.repository} ${metadata.repository}/tree/HEAD{/dir}`),
		);
		assert.ok(page.includes('href="../"'));
		assert.ok(!page.includes('<script'));
		const actual = goMetadata(resolve(import.meta.dirname, '../../..'));
		assert.equal(actual.modulePath, `${actual.root}/${actual.version}`);
	} finally {
		await rm(temporary, { recursive: true, force: true });
	}
});
