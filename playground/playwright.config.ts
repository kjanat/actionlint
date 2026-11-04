import { defineConfig } from '@playwright/test';

export default defineConfig({
	testDir: './tests',
	workers: 1,
	timeout: 60_000,
	use: { browserName: 'chromium', headless: true },
	outputDir: `../.review-artifacts/browser-lsp/playground/${Date.now()}`,
	reporter: 'list',
});
