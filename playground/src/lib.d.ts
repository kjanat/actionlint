interface ActionlintError {
	kind: string;
	message: string;
	line: number;
	column: number;
	// Column where actionlint's `^~~~~` indicator ends, inclusive. Equal to column when there is none.
	endColumn: number;
}

interface Window {
	runShellcheck(args: string[], script: string): Promise<string>;
	actionlintCatalogue?: string;
	runActionlint?(src: string): void;
	getYamlSource(): string;
	showError(msg: string): void;
	onCheckCompleted(errs: ActionlintError[], source: string): void;
	dismissLoading(): void;
}

declare class Go {
	importObject: WebAssembly.Imports;
	run(mod: WebAssembly.Instance): Promise<unknown>;
}

declare module '*wasm_exec.js';

// The published 0.11.0 wrapper has no declarations. Keep its narrow boundary
// typed here; result validation is shared by the worker and integration tests.
declare module '@kjanat/rshellcheck-wasm' {
	export function createChecker(bytes: BufferSource): Promise<{
		check(request: import('./shellcheck-request').ShellcheckRequest): unknown;
		close(): void;
	}>;
}

declare module '*?url' {
	const url: string;
	export default url;
}
