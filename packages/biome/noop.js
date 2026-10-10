#!/usr/bin/env node
/**
 * this stub exists to prevent npx from executing https://npm.im/biome
 * instead of the mise-managed @biomejs/biome binary.
 */

import * as a from 'ansispeck';

/** @param {string} s
 * @param {"red" | "green"} c
 * @returns {string} */
// biome-ignore lint/performance/noDynamicNamespaceImportAccess: shut up
const code = (s, c) => c ? a[c](s) : a.bold(s);

if (import.meta.main) {
	const msg = `You just ran ${code('npx biome', 'red')} which is not the correct way to run Biome.
Either run: ${code('npx @biomejs/biome', 'green')}, or ${code('mise exec -- biome', 'green')}

Install it globally with ${code('npm install -g @biomejs/biome', 'green')} or ${code('mise use -g biome', 'green')}.`;

	console.error(msg);
	console.error(`See ${a.link('https://biomejs.dev/docs/usage')} for more information.`);
	process.exit(1);
}
