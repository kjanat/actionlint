/// <reference types="vitest/config" />
import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { copyFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { defineConfig } from 'vite';

const here = import.meta.dirname;
const oneUp = dirname(here);
const outDir = resolve(here, 'dist');

const manual = resolve(oneUp, 'man/actionlint.1.html');
const manualStyle = resolve(oneUp, 'man/manual.css');

function buildVersion() {
	const repository = 'https://github.com/kjanat/actionlint';
	try {
		const git = (...args: string[]) =>
			execFileSync('git', args, { cwd: oneUp, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
		const ref = git('rev-parse', 'HEAD');
		const version = git('describe', '--tags', '--match', 'v[0-9]*.[0-9]*.[0-9]*', '--always', '--dirty');
		const url = /^v\d+\.\d+\.\d+$/.test(version)
			? `${repository}/releases/tag/${version}`
			: `${repository}/tree/${ref}`;
		return { version, ref, url };
	} catch {
		return { version: 'development', ref: 'HEAD', url: repository };
	}
}

const { version, ref, url } = buildVersion();

// The deployed site is the bundle plus a 404 fallback and the rendered command manual.
function sitePages(): import('vite').Plugin {
	return {
		name: 'actionlint:site-pages',
		apply: 'build',
		async closeBundle() {
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
			name: 'actionlint:version',
			transformIndexHtml: html =>
				html.replaceAll('%ACTIONLINT_VERSION%', version)
					.replaceAll('%ACTIONLINT_VERSION_URL%', url).replaceAll('%ACTIONLINT_REF%', ref),
		},
		sitePages(),
	],
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
