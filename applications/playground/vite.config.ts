/// <reference types="vitest/config" />

import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { copyFile, realpath } from 'node:fs/promises';
import { resolve } from 'node:path';

import { defaultClientConditions, defineConfig, searchForWorkspaceRoot } from 'vite';

import { buildVersion, renderBuildVersion } from './src/build-version.ts';

const here = import.meta.dirname;
const repositoryRoot = resolve(here, '../..');
const outDir = resolve(here, 'dist');

const manual = resolve(repositoryRoot, 'man/actionlint.1.html');
const manualStyle = resolve(repositoryRoot, 'man/manual.css');

const version = buildVersion(repositoryRoot, process.env.ACTIONLINT_LATEST_RELEASE);

function buildLanguageServer(): void {
	execFileSync('bun', ['run', '--cwd', resolve(repositoryRoot, 'libraries/browser-lsp'), 'build'], {
		cwd: repositoryRoot,
		stdio: 'inherit',
	});
}

function browserLanguageServer(): import('vite').Plugin {
	return {
		name: 'actionlint:browser-language-server',
		apply: (_config, environment) => environment.mode !== 'test',
		buildStart() {
			buildLanguageServer();
		},
	};
}

// The deployed site is the bundle plus a 404 fallback and the rendered command manual.
function sitePages(): import('vite').Plugin {
	return {
		name: 'actionlint:site-pages',
		apply: 'build',
		async writeBundle() {
			await copyFile(resolve(outDir, 'index.html'), resolve(outDir, '404.html'));

			if (!existsSync(manual)) {
				this.warn(`${manual} does not exist, so man.html and usage.html are not emitted. Run \`mise run man\` first.`);
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
		browserLanguageServer(),
		{
			name: 'actionlint:workspace-reader',
			apply: 'build',
			async buildStart() {
				const reader = await this.resolve('@kjlint/changelog-rss', resolve(here, 'github-changelog/index.html'));
				const expected = await realpath(resolve(repositoryRoot, 'libraries/github-changelog/src/index.ts'));
				if (!reader || reader.external || await realpath(reader.id) !== expected) {
					this.error(
						'The changelog reader must resolve to this checkout’s libraries/github-changelog/src/index.ts. Check the workspace dependency and source export condition.',
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
	resolve: {
		conditions: ['source', ...defaultClientConditions],
	},
	server: { fs: { allow: [searchForWorkspaceRoot(here)] } },
	build: {
		outDir,
		emptyOutDir: true,
		sourcemap: true,
		rollupOptions: {
			external: (source, importer) =>
				Boolean(importer?.endsWith('/xml-viewer.ts'))
				&& (source.startsWith('@codemirror/') || source === '@lezer/highlight' || source === 'xml-formatter'),
			input: {
				main: resolve(here, 'index.html'),
				changelog: resolve(here, 'github-changelog/index.html'),
			},
		},
	},
	test: {
		include: ['src/test.ts', 'src/*.spec.ts'],
		environment: 'jsdom',
	},
});
