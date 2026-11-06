import { readFile } from 'node:fs/promises';
import { createServer, type Server } from 'node:http';
import { extname, resolve, sep } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import type { BrowserLanguageClient, createBrowserLanguageClient, PublishDiagnosticsParams } from '../../src/index.ts';

interface BrowserSession {
	client: BrowserLanguageClient;
	publications: PublishDiagnosticsParams[];
	errors: string[];
	files: Record<string, string>;
	uri: string;
	version: number;
	opened: boolean;
	reads: string[];
	holdReads: boolean;
	cancelledReads: number;
}

declare global {
	interface Window {
		browserSession?: BrowserSession;
	}
}

let server: Server;
let origin: string;
const assets = resolve(import.meta.dirname, '../../dist');
const types: Record<string, string> = { '.js': 'text/javascript', '.wasm': 'application/wasm', '.html': 'text/html' };

test.beforeAll(async () => {
	server = createServer((request, response) => {
		void (async () => {
			const url = new URL(request.url ?? '/', 'http://localhost');
			if (url.pathname === '/') {
				response.setHeader('Content-Type', 'text/html');
				response.end('<!doctype html><title>Browser language server test</title>');
				return;
			}
			const file = resolve(assets, `.${url.pathname}`);
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
		})().catch(error => {
			response.writeHead(500).end(String(error));
		});
	});
	await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
	const address = server.address();
	if (!address || typeof address === 'string') throw new Error('Could not start the browser fixture');
	origin = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
	await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
});

async function start(page: Page, root = 'file:///workspace', files: Record<string, string> = {}) {
	await page.goto(origin);
	return page.evaluate(async ({ origin, root, files }) => {
		function isClientModule(
			value: unknown,
		): value is { createBrowserLanguageClient: typeof createBrowserLanguageClient } {
			return typeof value === 'object' && value !== null && 'createBrowserLanguageClient' in value
				&& typeof value.createBrowserLanguageClient === 'function';
		}
		const module: unknown = await import(`${origin}/index.js`);
		if (!isClientModule(module)) throw new Error('The browser package has no client factory');
		const publications: PublishDiagnosticsParams[] = [];
		const errors: string[] = [];
		const worker = new Worker(`${origin}/worker.js`, { type: 'module' });
		const reads: string[] = [];
		const client = module.createBrowserLanguageClient(worker, {
			async readFile(uri, cancellation) {
				reads.push(uri);
				const session = window.browserSession;
				if (session?.holdReads) {
					await new Promise<void>(resolve => {
						const listener = cancellation.onCancellationRequested(() => {
							session.cancelledReads++;
							listener.dispose();
							resolve();
						});
					});
				}
				return files[uri] ?? null;
			},
		}, {
			rootURI: root,
			onDiagnostics: params => publications.push(params),
			onError: message => errors.push(message),
		});
		window.browserSession = {
			client,
			publications,
			errors,
			files,
			uri: `${root}/.github/workflows/test.yml`,
			version: 0,
			opened: false,
			reads,
			holdReads: false,
			cancelledReads: 0,
		};
		return (await client.initialize()).capabilities;
	}, { origin, root, files });
}

async function check(page: Page, source: string) {
	const version = await page.evaluate(async source => {
		const session = window.browserSession;
		if (!session) throw new Error('No browser language-server session');
		const version = ++session.version;
		if (session.opened) await session.client.change(session.uri, source, version);
		else {
			await session.client.open(session.uri, source, version);
			session.opened = true;
		}
		return version;
	}, source);
	await page.waitForFunction(
		version => window.browserSession?.publications.some(params => params.version === version),
		version,
	);
	return page.evaluate(version => {
		const session = window.browserSession;
		const publication = session?.publications.findLast(params => params.version === version);
		if (!publication) throw new Error('No matching publication');
		if (session?.errors.length) throw new Error(session.errors.join('\n'));
		return publication.diagnostics;
	}, version);
}

test.afterEach(async ({ page }) => {
	await page.evaluate(async () => {
		await window.browserSession?.client.stop();
	});
});

for (const failure of ['wasm download', 'worker startup']) {
	test(`${failure} failure rejects initialization instead of leaving the client waiting`, async ({ page }) => {
		if (failure === 'wasm download') {
			await page.route('**/server.wasm', route => route.fulfill({ status: 503, body: 'Unavailable' }));
		} else {
			await page.route(
				'**/worker.js',
				route => route.fulfill({ contentType: 'text/javascript', body: 'throw new Error("Test startup failure")' }),
			);
		}
		await expect(start(page)).rejects.toThrow();
		await expect.poll(() => page.evaluate(() => window.browserSession?.errors.length)).toBeGreaterThan(0);
	});
}

test('real WebAssembly server publishes versioned diagnostics and clears them after edits', async ({ page }) => {
	const capabilities = await start(page);
	expect(capabilities.positionEncoding).toBe('utf-16');
	expect(capabilities.codeActionProvider).toBe(true);
	expect(capabilities.completionProvider).toBeTruthy();
	const missingRunner = await check(page, 'on: push\njobs:\n  test:\n    steps:\n      - run: echo hi\n');
	expect(missingRunner.some(d => d.code === 'syntax-check' && String(d.message).includes('runs-on'))).toBe(true);
	const events = await check(
		page,
		'on: foo\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n',
	);
	const event = events.find(d => d.code === 'events');
	expect(event?.range).toEqual({ start: { line: 0, character: 4 }, end: { line: 0, character: 7 } });
	expect(event?.severity).toBe(1);
	expect(await check(page, 'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n'))
		.toEqual([]);
	const information = await check(
		page,
		'on: push # ignore[expression]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n',
	);
	expect(information.find(d => d.code === 'unused-suppression')?.severity).toBe(3);
});

test('closed action metadata is read through the virtual workspace host', async ({ page }) => {
	const root = 'vscode-vfs://github/owner/repository';
	await start(page, root, {
		[`${root}/local/action.yml`]:
			'name: Local\ndescription: Fixture\ninputs:\n  valid:\n    description: Accepted input\nruns:\n  using: composite\n  steps:\n    - run: echo hi\n      shell: bash\n',
	});
	const diagnostics = await check(
		page,
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n        with:\n          invalid: value\n',
	);
	expect(diagnostics.some(d => String(d.message).includes('invalid') && String(d.message).includes('input'))).toBe(
		true,
	);
	expect(await page.evaluate(() => window.browserSession?.reads)).toContain(`${root}/local/action.yml`);
});

test('JavaScript action entry files are checked through the virtual workspace host', async ({ page }) => {
	const root = 'vscode-vfs://github/owner/repository';
	const entry = `${root}/local/index.js`;
	await start(page, root, {
		[`${root}/local/action.yml`]: 'name: Local\ndescription: Fixture\nruns: {using: node24, main: index.js}\n',
		[entry]: 'export {};\n',
	});
	const workflow = 'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n';
	expect(await check(page, workflow)).toEqual([]);
	expect(await page.evaluate(() => window.browserSession?.reads)).toContain(entry);
	await page.evaluate(uri => {
		const session = window.browserSession;
		if (!session) throw new Error('The browser session was not started');
		delete session.files[uri];
	}, entry);
	const missing = await check(page, workflow);
	expect(missing.some(diagnostic => {
		const message = typeof diagnostic.message === 'string' ? diagnostic.message : diagnostic.message.value;
		return message.includes('index.js') && message.includes('does not exist');
	})).toBe(true);
});

test('reusable workflow inputs and permission ceilings use virtual workspace metadata', async ({ page }) => {
	const root = 'vscode-vfs://github/owner/repository';
	const callee = `${root}/.github/workflows/callee.yml`;
	await start(page, root, {
		[callee]: 'on: {workflow_call: {inputs: {valid: {type: string}}}}\n'
			+ 'jobs:\n  build:\n    runs-on: ubuntu-latest\n    permissions: {contents: write}\n    steps: [{run: echo ok}]\n',
	});
	const source =
		'on: push\npermissions: {}\njobs:\n  call:\n    uses: ./.github/workflows/callee.yml\n    with: {invalid: value}\n';
	const diagnostics = await check(page, source);
	const callMessages = diagnostics.filter(d => d.code === 'workflow-call')
		.map(d => typeof d.message === 'string' ? d.message : d.message.value);
	expect(callMessages.some(message => message.includes('input "invalid"'))).toBe(true);
	expect(callMessages.some(message => message.includes('contents: none'))).toBe(true);
	expect(await page.evaluate(() => window.browserSession?.reads)).toContain(callee);
	expect(
		await check(
			page,
			source.replace('permissions: {}', 'permissions: write-all').replace('invalid: value', 'valid: value'),
		),
	)
		.toEqual([]);
});

test('new edits cancel outstanding host reads and cannot publish old findings', async ({ page }) => {
	await start(page);
	await page.evaluate(async () => {
		const session = window.browserSession;
		if (!session) throw new Error('No session');
		session.holdReads = true;
		session.opened = true;
		await session.client.open(
			session.uri,
			'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./slow\n',
			++session.version,
		);
	});
	await page.waitForFunction(() => window.browserSession?.reads.some(uri => uri.includes('/slow/action.')));
	await page.evaluate(() => {
		if (window.browserSession) window.browserSession.holdReads = false;
	});
	expect(await check(page, 'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n'))
		.toEqual([]);
	await expect.poll(() => page.evaluate(() => window.browserSession?.cancelledReads)).toBeGreaterThan(0);
	expect(await page.evaluate(() => window.browserSession?.publications.some(params => params.version === 1))).toBe(
		false,
	);
});

test('virtual workspace URIs survive diagnostics, completion and versioned quick fixes', async ({ page }) => {
	await start(page, 'vscode-vfs://github/owner/repository');
	const diagnostics = await check(
		page,
		'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: https://github.com/actions/checkout@v7\n',
	);
	expect(diagnostics.some(d => d.code === 'vendor-lock-in')).toBe(true);
	const result = await page.evaluate(async diagnostics => {
		const session = window.browserSession;
		if (!session) throw new Error('No session');
		return session.client.codeActions({
			textDocument: { uri: session.uri },
			range: { start: { line: 5, character: 0 }, end: { line: 5, character: 60 } },
			context: { diagnostics },
		});
	}, diagnostics);
	const action = result?.find(action => 'edit' in action && action.isPreferred);
	expect(action && 'edit' in action ? action.edit?.documentChanges : undefined).toEqual([
		expect.objectContaining({
			textDocument: { uri: 'vscode-vfs://github/owner/repository/.github/workflows/test.yml', version: 1 },
			edits: expect.any(Array),
		}),
	]);
	const completions = await page.evaluate(async () => {
		const session = window.browserSession;
		if (!session) throw new Error('No session');
		const source = 'on: push\nrun-name: ${{ github. }}\njobs: {}\n';
		await session.client.change(session.uri, source, ++session.version);
		return session.client.complete({ textDocument: { uri: session.uri }, position: { line: 1, character: 21 } });
	});
	expect(
		completions && !Array.isArray(completions) ? completions.items.some(item => item.label === 'repository') : false,
	).toBe(true);
});
