import { strict as assert } from 'node:assert';
import { afterEach, beforeEach, describe, it, vi } from 'vitest';

const workers: FakeWorker[] = [];
class FakeWorker {
	onmessage?: (event: { data: unknown }) => void;
	onerror?: (event: { message: string }) => void;
	terminate = vi.fn();
	postMessage = vi.fn((message: { id: number; wasm?: ArrayBuffer }) => {
		if (message.wasm) {
			queueMicrotask(() => {
				this.onmessage?.({ data: { id: message.id, ready: true } });
			});
		}
	});
	constructor(readonly url: string, readonly options: WorkerOptions) {
		workers.push(this);
	}
}

import pkg from '../package.json';
import { loadShellcheck, shellcheckAssets } from './shellcheck';
import { shellcheckOutput, shellcheckRequest } from './shellcheck-request';

const originalURL = URL;
const createObjectURL = vi.fn(() => 'blob:test-worker');
const revokeObjectURL = vi.fn();

describe('ShellCheck worker boundary', () => {
	beforeEach(() => {
		workers.length = 0;
		createObjectURL.mockClear();
		revokeObjectURL.mockClear();
		vi.stubGlobal('Worker', FakeWorker);
		vi.stubGlobal(
			'URL',
			class extends originalURL {
				static override createObjectURL = createObjectURL;
				static override revokeObjectURL = revokeObjectURL;
			},
		);
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
		const { check } = await loadShellcheck();
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
		const { check } = await loadShellcheck();
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
		const { check } = await loadShellcheck();
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

	it('loads the pinned CDN assets through a module bootstrap, matching offline tests', async () => {
		await loadShellcheck();
		const version = pkg.devDependencies['@kjanat/rshellcheck-wasm'];
		assert.equal(
			shellcheckAssets.wasm,
			`https://cdn.jsdelivr.net/npm/@kjanat/rshellcheck-wasm@${version}/shellcheck_wasm.wasm`,
		);
		assert.equal(
			shellcheckAssets.worker,
			`https://cdn.jsdelivr.net/npm/@kjanat/rshellcheck-wasm@${version}/worker.mjs`,
		);
		assert.deepEqual(vi.mocked(fetch).mock.calls, [[shellcheckAssets.wasm]]);
		assert.equal(workers[0]?.url, 'blob:test-worker');
		assert.deepEqual(workers[0].options, { type: 'module' });
		assert.deepEqual(revokeObjectURL.mock.calls, [['blob:test-worker']]);
	});

	it('gives a new edit its own deadline and ignores events from the cancelled worker', async () => {
		const client = await loadShellcheck();
		vi.useFakeTimers();
		const old = assert.rejects(client.check(['-'], 'echo old'), /superseded/);
		await vi.advanceTimersByTimeAsync(9_900);
		client.cancel();
		await old;
		const current = client.check(['-'], 'echo current');
		await vi.advanceTimersByTimeAsync(0);
		assert.equal(workers.length, 2, 'busy worker must be replaced for the new edit');
		workers[0]?.onerror?.({ message: 'late error from obsolete worker' });
		await vi.advanceTimersByTimeAsync(1_000);
		assert.equal(workers[1]?.terminate.mock.calls.length, 0, 'old deadline must not terminate the new edit');
		workers[1].onmessage?.({ data: { id: 4, result: { comments: [] } } });
		assert.equal(await current, '{"comments":[]}');
		assert.equal(vi.getTimerCount(), 0, 'completion must release every request timer');
	});

	it('cancels calls waiting for initialization without sending their obsolete scripts', async () => {
		const client = await loadShellcheck();
		// check() has reached its await but has not sent a script yet.
		const old = assert.rejects(client.check(['-'], 'echo obsolete'), /superseded/);
		client.cancel();
		await old;
		assert.equal(workers[0]?.postMessage.mock.calls.length, 1, 'only initialization should have been sent');
		const current = client.check(['-'], 'echo current');
		await Promise.resolve();
		assert.equal(workers.length, 1, 'an idle initialized worker should be reused');
		workers[0].onmessage?.({ data: { id: 2, result: { comments: [] } } });
		assert.equal(await current, '{"comments":[]}');
	});

	it('cancels replacement initialization and permits another edit to recover', async () => {
		const client = await loadShellcheck();
		const first = assert.rejects(client.check(['-'], 'echo first'), /superseded/);
		await Promise.resolve();
		client.cancel();
		await first;
		const second = assert.rejects(client.check(['-'], 'echo second'), /superseded/);
		client.cancel();
		await second;
		const third = client.check(['-'], 'echo third');
		await new Promise(resolve => setTimeout(resolve, 0));
		assert.equal(workers.length, 3);
		workers[2]?.onmessage?.({ data: { id: 5, result: { comments: [] } } });
		assert.equal(await third, '{"comments":[]}');
	});
});
