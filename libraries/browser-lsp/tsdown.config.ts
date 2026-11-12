import { defineConfig } from 'tsdown';

export default defineConfig({
	entry: { index: 'src/index.ts', worker: 'src/worker.ts' },
	format: 'esm',
	platform: 'browser',
	target: 'esnext',
	fixedExtension: false,
	dts: true,
	clean: true,
	deps: {
		alwaysBundle: [/^vscode-/],
		neverBundle: ['./wasm_exec.js'],
		onlyBundle: [/^vscode-/],
		dts: { neverBundle: true },
	},
});
