import { readFile } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { createRequire } from 'node:module';
import { dirname, extname, resolve, sep } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

const assets = resolve(import.meta.dirname, '../dist');
const require = createRequire(import.meta.url);
const shellcheck = dirname(require.resolve('@kjanat/rshellcheck-wasm/worker'));
const types: Record<string, string> = {
	'.js': 'text/javascript',
	'.mjs': 'text/javascript',
	'.wasm': 'application/wasm',
	'.css': 'text/css',
	'.html': 'text/html',
	'.woff2': 'font/woff2',
};
let server: Server;
let origin: string;

test.beforeAll(async () => {
	server = createServer((request, response) => {
		void (async () => {
			const pathname = new URL(request.url ?? '/', 'http://localhost').pathname;
			const file = resolve(assets, `.${pathname === '/' ? '/index.html' : pathname}`);
			if (!file.startsWith(`${assets}${sep}`)) {
				response.writeHead(403).end();
				return;
			}
			try {
				response.setHeader('Content-Type', types[extname(file)] ?? 'application/octet-stream');
				response.end(await readFile(file));
			} catch {
				response.writeHead(404).end();
			}
		})().catch((error: unknown) => {
			response.writeHead(500).end(String(error));
		});
	});
	await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
	const address = server.address();
	if (!address || typeof address === 'string') throw new Error('Could not start the built playground fixture');
	origin = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
	await new Promise<void>((resolve, reject) =>
		server.close(error => {
			if (error) reject(error);
			else resolve();
		})
	);
});

test.beforeEach(async ({ context }) => {
	// Serve the installed, version-matched runtime without requiring the CDN.
	await context.route('https://cdn.jsdelivr.net/npm/@kjanat/rshellcheck-wasm@*/**', async route => {
		const name = new URL(route.request().url()).pathname.split('/').at(-1);
		if (!name || !['worker.mjs', 'shellcheck.mjs', 'shellcheck_wasm.wasm', 'LICENSE'].includes(name)) {
			await route.abort();
			return;
		}
		await route.fulfill({
			body: await readFile(resolve(shellcheck, name)),
			contentType: types[extname(name)] ?? 'text/plain',
			headers: { 'Access-Control-Allow-Origin': '*' },
		});
	});
});

function workflow(steps: string): string {
	return `on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n${steps}\n`;
}

async function open(page: Page, source: string): Promise<void> {
	await page.goto(`${origin}/?${new URLSearchParams({ s: source })}`);
	await expect(page.locator('#loading')).toBeHidden();
}

async function edit(page: Page, source: string): Promise<void> {
	await page.locator('.cm-content').fill(source);
}

test('built playground uses real ShellCheck ranges, suppressions and shell selection', async ({ page }) => {
	for (const step of ['      - run: |\n          echo $value', '      - run: "echo $value"']) {
		await open(page, workflow(step));
		const row = page.locator('#lint-result-body tr').filter({ hasText: 'SC2086' });
		await expect(row).toHaveCount(1);
		await row.click();
		await expect(page.locator('.cm-lintRange')).toContainText('$value');
	}
	for (
		const step of [
			'      - run: |\n          # shellcheck disable=SC2086\n          echo $value',
			'      # actionlint:ignore-next-line shellcheck\n      - run: echo $value',
			'      - shell: python\n        run: print("hello")',
			'      - shell: pwsh\n        run: Write-Output $value',
		]
	) {
		await edit(page, workflow(step));
		await expect(page.locator('#success-msg')).toBeVisible();
		await expect(page.locator('#lint-result-body tr')).toHaveCount(0);
	}
	await edit(page, workflow('      - run: echo "${{ github.ref }}"'));
	await expect(page.locator('#lint-result-body tr').filter({ hasText: 'script-interpolation' })).toHaveCount(1);
	await expect(page.locator('#lint-result-body tr').filter({ hasText: 'SC2086' })).toHaveCount(0);
	await edit(page, workflow('      - shell: sh\n        run: "[[ -n x ]]"'));
	await expect(page.locator('#lint-result-body tr').filter({ hasText: 'SC3010' })).toHaveCount(1);
});

test('built editor displays information and applies a server quick fix', async ({ page }) => {
	await open(
		page,
		workflow('      - uses: https://github.com/actions/checkout@v7').replace(
			'on: push',
			'on: push # ignore[expression]',
		),
	);
	await expect(page.locator('#lint-result-body tr[data-severity="info"]').filter({ hasText: 'unused-suppression' }))
		.toHaveCount(1);
	const row = page.locator('#lint-result-body tr').filter({ hasText: 'vendor-lock-in' });
	await expect(row).toHaveCount(1);
	await page.screenshot({ path: test.info().outputPath('diagnostics.png'), fullPage: true });
	await row.getByRole('button').first().click();
	await expect(page.locator('.cm-content')).toContainText('uses: actions/checkout@v7');
	await expect(page.locator('#lint-result-body tr').filter({ hasText: 'vendor-lock-in' })).toHaveCount(0);
	await edit(page, workflow('      - run: echo hi'));
	await expect(page.locator('#success-msg')).toBeVisible();
});

test('startup failures are visible and never display a clean analysis', async ({ page }) => {
	await page.route('**/shellcheck_wasm.wasm', route => route.fulfill({ status: 503, body: 'Unavailable' }));
	await open(page, workflow('      - run: echo hi'));
	await expect(page.locator('#error-msg')).toContainText('HTTP 503');
	await expect(page.locator('#success-msg')).toBeHidden();
});
