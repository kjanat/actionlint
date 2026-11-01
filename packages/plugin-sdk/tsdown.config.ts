import { defineConfig } from 'tsdown';

export default defineConfig({
	entry: { '*': ['src/*.ts', '!src/image-report.ts'] },
	format: 'esm',
	platform: 'neutral',
	target: 'esnext',
	fixedExtension: false,
	dts: true,
	clean: false,
	deps: { neverBundle: true },
});
