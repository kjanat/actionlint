import pkg from '../package.json';
import { shellcheckOutput, shellcheckRequest } from './shellcheck-request';

const cdn = `https://cdn.jsdelivr.net/npm/@kjanat/rshellcheck-wasm@${pkg.devDependencies['@kjanat/rshellcheck-wasm']}`;
export const shellcheckAssets = {
	wasm: `${cdn}/shellcheck_wasm.wasm`,
	worker: `${cdn}/worker.mjs`,
	license: `${cdn}/LICENSE`,
};

export interface ShellcheckClient {
	check: Window['runShellcheck'];
	cancel(): void;
}

// Bound untrusted analysis in a worker. A timeout/trap discards the instance;
// the next lint can start a fresh one without reloading the editor.
export async function loadShellcheck(): Promise<ShellcheckClient> {
	const response = await fetch(shellcheckAssets.wasm);
	if (!response.ok) throw new Error(`Could not load ShellCheck WASM: HTTP ${response.status}`);
	const wasm = await response.arrayBuffer();
	let worker: Worker | undefined;
	let bootstrap: string | undefined;
	let ready: Promise<unknown> | undefined;
	let sequence = 0;
	let generation = 0;
	const pending = new Map<
		number,
		{ resolve(value: unknown): void; reject(error: Error): void; timer: ReturnType<typeof setTimeout> }
	>();

	function releaseBootstrap(): void {
		if (bootstrap) URL.revokeObjectURL(bootstrap);
		bootstrap = undefined;
	}

	function stop(error: Error): void {
		generation++;
		worker?.terminate();
		releaseBootstrap();
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
		// Worker entry URLs must be same-origin. This module bootstrap imports the
		// pinned CDN worker, whose relative import loads shellcheck.mjs from that CDN.
		bootstrap = URL.createObjectURL(
			new Blob([`import ${JSON.stringify(shellcheckAssets.worker)};`], { type: 'text/javascript' }),
		);
		try {
			worker = new Worker(bootstrap, { type: 'module' });
		} catch (error) {
			releaseBootstrap();
			throw error;
		}
		const current = worker;
		worker.onmessage = ({ data }: MessageEvent<{ id: number; ready?: boolean; result?: unknown; error?: string }>) => {
			if (worker !== current) return;
			const entry = pending.get(data.id);
			if (!entry) return;
			if (data.error) {
				stop(new Error(data.error));
				return;
			}
			clearTimeout(entry.timer);
			pending.delete(data.id);
			if (data.ready) releaseBootstrap();
			entry.resolve(data.ready ?? data.result);
		};
		worker.onerror = event => {
			if (worker !== current) return;
			stop(new Error(event.message || 'ShellCheck worker failed'));
		};
		worker.onmessageerror = () => {
			if (worker !== current) return;
			stop(new Error('Could not decode ShellCheck worker response'));
		};
		const initialization = send({ wasm });
		ready = initialization;
		void initialization.catch(() => {
			if (ready === initialization) ready = undefined;
		});
		return initialization;
	}

	await initialize();
	return {
		async check(args, script) {
			const request = shellcheckRequest(args, script);
			const current = generation;
			await initialize();
			if (current !== generation) throw new Error('ShellCheck analysis was superseded');
			return shellcheckOutput(await send({ request }));
		},
		cancel() {
			// Invalidate calls between worker initialization and send too.
			if (pending.size > 0) stop(new Error('ShellCheck analysis was superseded'));
			else generation++;
		},
	};
}
