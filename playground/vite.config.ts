/// <reference types="vitest/config" />
import { existsSync } from 'node:fs';
import { copyFile, realpath } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';

import { defaultClientConditions, defineConfig } from 'vite';

import { buildVersion, renderBuildVersion } from './src/build-version.ts';

const here = import.meta.dirname;
const oneUp = dirname(here);
const outDir = resolve(here, 'dist');

const manual = resolve(oneUp, 'man/actionlint.1.html');
const manualStyle = resolve(oneUp, 'man/manual.css');

const version = buildVersion(oneUp, process.env.ACTIONLINT_LATEST_RELEASE);

// The deployed site is the bundle plus a 404 fallback and the rendered command manual.
function sitePages(): import('vite').Plugin {
	return {
		name: 'actionlint:site-pages',
		apply: 'build',
		async writeBundle() {
			await copyFile(resolve(outDir, 'index.html'), resolve(outDir, '404.html'));

			if (!existsSync(manual)) {
				this.warn(`${manual} does not exist, so man.html and usage.html are not emitted. Run \`make man\` first.`);
				return;
			}
			await copyFile(manual, resolve(outDir, 'man.html'));
			await copyFile(manual, resolve(outDir, 'usage.html'));
			await copyFile(manualStyle, resolve(outDir, 'manual.css'));
		},
	};
}

export default defineConfig({
	base: './',
	plugins: [
		{
			name: 'actionlint:workspace-reader',
			apply: 'build',
			async buildStart() {
				const reader = await this.resolve('@kjlint/changelog-rss', resolve(here, 'github-changelog/index.html'));
				const expected = await realpath(resolve(oneUp, 'packages/changelog-feed/src/index.ts'));
				if (!reader || reader.external || await realpath(reader.id) !== expected) {
					this.error(
						'The changelog reader must resolve to this checkout’s packages/changelog-feed/src/index.ts. Check the workspace dependency and source export condition.',
					);
				}
			},
		},
		{
			name: 'actionlint:version',
			transformIndexHtml: html => renderBuildVersion(html, version),
		},
		sitePages(),
	],
	resolve: { conditions: ['source', ...defaultClientConditions] },
	build: {
		outDir,
		emptyOutDir: true,
		sourcemap: true,
		rollupOptions: {
			input: {
				main: resolve(here, 'index.html'),
				changelog: resolve(here, 'github-changelog/index.html'),
			},
		},
	},
	test: {
		include: ['src/test.ts'],
		environment: 'jsdom',
	},
});
