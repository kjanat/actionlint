// @ts-check

import eslint from '@eslint/js';
import { defineConfig } from 'eslint/config';
import ts from 'typescript-eslint';

export default defineConfig(
	// dist is build output and public holds the Go toolchain's wasm runtime glue.
	{ ignores: ['dist/**', 'github-changelog/*.js', 'public/**'] },
	eslint.configs.recommended,
	ts.configs.strictTypeChecked,
	{
		files: ['src/**/*.ts', 'vite.config.ts'],
		languageOptions: {
			parserOptions: {
				projectService: true,
				tsconfigRootDir: import.meta.dirname,
			},
		},
	},
	{
		files: ['src/**/*.ts', 'vite.config.ts', 'eslint.config.mjs'],
		rules: {
			eqeqeq: ['error', 'always'],
			'@typescript-eslint/no-unnecessary-condition': ['error', { allowConstantLoopConditions: true }],
			'@typescript-eslint/no-unsafe-member-access': 'off',
			'@typescript-eslint/no-unsafe-argument': 'off',
			'@typescript-eslint/no-unsafe-assignment': 'off',
			'@typescript-eslint/restrict-template-expressions': 'off',
		},
	},
	{
		files: ['src/test.ts'],
		rules: {
			'@typescript-eslint/unbound-method': 'off', // For checking `window.runActionlint`
		},
	},
	{
		files: ['eslint.config.mjs'],
		languageOptions: {
			parserOptions: {
				projectService: false,
				project: 'tsconfig.json',
			},
		},
	},
);
