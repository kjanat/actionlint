import { strict as assert } from 'node:assert';
import { afterEach, beforeEach, describe, it, vi } from 'vitest';

const workers = vi.hoisted(() =>
	[] as {
		onmessage?: (event: { data: unknown }) => void;
		postMessage: ReturnType<typeof vi.fn>;
		terminate: ReturnType<typeof vi.fn>;
	}[]
);

vi.mock('@kjanat/rshellcheck-wasm/worker?worker', () => ({
	default: class {
		onmessage?: (event: { data: unknown }) => void;
		terminate = vi.fn();
		postMessage = vi.fn((message: { id: number; wasm?: ArrayBuffer }) => {
			if (message.wasm) {
				queueMicrotask(() => {
					this.onmessage?.({ data: { id: message.id, ready: true } });
				});
			}
		});
		constructor() {
			workers.push(this);
		}
	},
}));

import { loadShellcheck } from './shellcheck';
import { shellcheckOutput, shellcheckRequest } from './shellcheck-request';

describe('ShellCheck worker boundary', () => {
	beforeEach(() => {
		workers.length = 0;
		vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(new Uint8Array([0])))));
	});
	afterEach(() => {
		vi.unstubAllGlobals();
		vi.useRealTimers();
	});

	it('rejects failed downloads before starting a worker', async () => {
		vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(null, { status: 503 }))));
		await assert.rejects(loadShellcheck(), /HTTP 503/);
		assert.equal(workers.length, 0);
	});

	it('correlates out-of-order replies from different scripts', async () => {
		const check = await loadShellcheck();
		const first = check(['-'], 'echo one');
		const second = check(['-'], 'echo two');
		await Promise.resolve();
		const worker = workers[0];
		assert.ok(worker);
		worker.onmessage?.({ data: { id: 3, result: { comments: [], marker: 'two' } } });
		worker.onmessage?.({ data: { id: 2, result: { comments: [], marker: 'one' } } });
		assert.match(await first, /one/);
		assert.match(await second, /two/);
	});

	it('terminates timed-out analysis and starts a fresh worker on retry', async () => {
		const check = await loadShellcheck();
		vi.useFakeTimers();
		const failed = assert.rejects(check(['-'], 'echo slow'), /deadline/);
		await vi.advanceTimersByTimeAsync(10_000);
		await failed;
		assert.equal(workers[0]?.terminate.mock.calls.length, 1);
		vi.useRealTimers();
		const retry = check(['-'], 'echo retry');
		await new Promise(resolve => setTimeout(resolve, 0));
		assert.equal(workers.length, 2);
		workers[1]?.onmessage?.({ data: { id: 4, result: { comments: [], dataflowError: null } } });
		assert.equal(await retry, '{"comments":[],"dataflowError":null}');
	});

	it('worker errors reject every pending request', async () => {
		const check = await loadShellcheck();
		const first = assert.rejects(check(['-'], 'echo one'), /WASM trap/);
		const second = assert.rejects(check(['-'], 'echo two'), /WASM trap/);
		await Promise.resolve();
		workers[0]?.onmessage?.({ data: { id: 2, error: 'WASM trap' } });
		await Promise.all([first, second]);
		assert.equal(workers[0]?.terminate.mock.calls.length, 1);
	});

	it('maps supported options and refuses partial analysis', () => {
		assert.deepEqual(
			shellcheckRequest(['--norc', '-f', 'json1', '--shell', 'sh', '-e', 'SC2154,SC1091', '-'], 'echo x'),
			{
				script: 'echo x',
				filename: '-',
				shell: 'sh',
				exclude: [2154, 1091],
			},
		);
		assert.throws(() => shellcheckRequest(['--rcfile', '/secret'], ''), /Unsupported/);
		assert.throws(() => shellcheckRequest(['--shell', 'python'], ''), /dialect/);
		assert.throws(() => shellcheckRequest(['-e', 'typo'], ''), /exclusions/);
		assert.throws(() => shellcheckOutput({ comments: [], dataflowError: 'budget' }), /analysis incomplete/);
		assert.throws(() => shellcheckOutput({}), /Invalid/);
	});
});
