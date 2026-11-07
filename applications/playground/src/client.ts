import type { BrowserClientOptions, BrowserLanguageClient } from '@kjanat-actionlint/language-server';
import workerURL from '@kjanat-actionlint/language-server/worker?worker&url';

import { loadShellcheck } from './shellcheck';

export const workspaceURI = 'file:///workspace';
export const documentURI = `${workspaceURI}/.github/workflows/playground.yml`;
export type PlaygroundClient = Pick<BrowserLanguageClient, 'open' | 'change' | 'complete' | 'codeActions' | 'stop'>;

/** Start the same worker a VS Code web LanguageClient can use directly. */
export async function startPlaygroundClient(
	options: Pick<BrowserClientOptions, 'onDiagnostics' | 'onError'>,
): Promise<PlaygroundClient> {
	const { createBrowserLanguageClient } = await import('@kjanat-actionlint/language-server');
	const shellcheck = await loadShellcheck();
	const worker = new Worker(workerURL, { type: 'module', name: 'actionlint language server' });
	const client = createBrowserLanguageClient(worker, {
		readFile: () => Promise.resolve(null),
		async runTool(name, args, stdin, cancellation) {
			if (name !== 'shellcheck') throw new Error(`Tool ${name} is not available in this playground`);
			const listener = cancellation.onCancellationRequested(() => {
				shellcheck.cancel();
			});
			try {
				return await shellcheck.check(args, stdin);
			} finally {
				listener.dispose();
			}
		},
	}, { ...options, rootURI: workspaceURI, tools: ['shellcheck'] });
	await client.initialize();
	return client;
}
