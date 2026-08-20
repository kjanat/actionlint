/// <reference types="vitest/config" />
import { existsSync } from 'node:fs';
import { copyFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { defineConfig } from 'vite';

const here = import.meta.dirname;
const oneUp = dirname(import.meta.dirname);
const outDir = resolve(here, 'dist');

const manual = resolve(oneUp, 'man/actionlint.1.html');
const manualStyle = resolve(oneUp, 'man/manual.css');

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
	plugins: [sitePages()],
	build: {
		outDir,
		emptyOutDir: true,
		sourcemap: true,
	},
	test: {
		include: ['src/test.ts'],
		environment: 'jsdom',
	},
});
