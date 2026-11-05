// @ts-check

import eslint from '@eslint/js';
import { defineConfig } from 'eslint/config';
import ts from 'typescript-eslint';

export default defineConfig(
	// Generated bundles and static assets are not editor source files.
	{ ignores: ['dist/**', 'github-changelog/*.js', 'public/**'] },
	eslint.configs.recommended,
	ts.configs.strictTypeChecked,
	{
		files: ['src/**/*.ts', 'tests/**/*.ts', 'playwright.config.ts', 'vite.config.ts'],
		languageOptions: {
			parserOptions: {
				projectService: true,
				tsconfigRootDir: import.meta.dirname,
			},
		},
	},
	{
		files: ['src/**/*.ts', 'tests/**/*.ts', 'playwright.config.ts', 'vite.config.ts', 'eslint.config.mjs'],
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
		files: ['**/*.{js,mjs,cjs}'],
		rules: {
			'@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
		},
		languageOptions: {
			parserOptions: {
				projectService: false,
				project: 'tsconfig.json',
				tsconfigRootDir: import.meta.dirname,
			},
		},
	},
);
