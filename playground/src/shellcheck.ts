import wasmUrl from '@kjanat/rshellcheck-wasm/wasm?url';
import ShellcheckWorker from '@kjanat/rshellcheck-wasm/worker?worker';

import { shellcheckOutput, shellcheckRequest } from './shellcheck-request';

// Bound untrusted analysis in a worker. A timeout/trap discards the instance;
// the next lint can start a fresh one without reloading the editor.
export async function loadShellcheck(): Promise<Window['runShellcheck']> {
	const response = await fetch(wasmUrl);
	if (!response.ok) throw new Error(`Could not load ShellCheck WASM: HTTP ${response.status}`);
	const wasm = await response.arrayBuffer();
	let worker: Worker | undefined;
	let ready: Promise<unknown> | undefined;
	let sequence = 0;
	const pending = new Map<
		number,
		{ resolve(value: unknown): void; reject(error: Error): void; timer: ReturnType<typeof setTimeout> }
	>();

	function stop(error: Error): void {
		worker?.terminate();
		worker = undefined;
		ready = undefined;
		for (const entry of pending.values()) {
			clearTimeout(entry.timer);
			entry.reject(error);
		}
		pending.clear();
	}

	function send(message: object): Promise<unknown> {
		return new Promise((resolve, reject) => {
			const id = ++sequence;
			const timer = setTimeout(() => {
				stop(new Error('ShellCheck exceeded its 10 second deadline'));
			}, 10_000);
			pending.set(id, { resolve, reject, timer });
			try {
				worker?.postMessage({ id, ...message });
			} catch (error) {
				stop(error instanceof Error ? error : new Error(String(error)));
			}
		});
	}

	function initialize(): Promise<unknown> {
		if (ready) return ready;
		worker = new ShellcheckWorker();
		worker.onmessage = ({ data }: MessageEvent<{ id: number; ready?: boolean; result?: unknown; error?: string }>) => {
			const entry = pending.get(data.id);
			if (!entry) return;
			if (data.error) {
				stop(new Error(data.error));
				return;
			}
			clearTimeout(entry.timer);
			pending.delete(data.id);
			entry.resolve(data.ready ?? data.result);
		};
		worker.onerror = event => {
			stop(new Error(event.message || 'ShellCheck worker failed'));
		};
		worker.onmessageerror = () => {
			stop(new Error('Could not decode ShellCheck worker response'));
		};
		ready = send({ wasm });
		return ready;
	}

	await initialize();
	return async (args, script) => {
		const request = shellcheckRequest(args, script);
		await initialize();
		return shellcheckOutput(await send({ request }));
	};
}
