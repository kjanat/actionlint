/// <reference types="vitest/config" />
import { existsSync } from 'node:fs';
import { copyFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(here, 'dist');
const manual = resolve(here, '../man/actionlint.1.html');
const manualStyle = resolve(here, '../man/dark.css');

// The deployed site is the bundle plus a 404 fallback and the rendered command manual.
function sitePages(): Plugin {
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
			await copyFile(manualStyle, resolve(outDir, 'dark.css'));
		},
	};
}

export default defineConfig({
	base: './',
	plugins: [sitePages()],
	build: {
		outDir: 'dist',
		emptyOutDir: true,
		sourcemap: true,
	},
	test: {
		include: ['src/test.ts'],
		environment: 'jsdom',
	},
});
