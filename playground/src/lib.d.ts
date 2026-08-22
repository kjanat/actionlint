interface ActionlintError {
	kind: string;
	message: string;
	line: number;
	column: number;
	// Column where actionlint's `^~~~~` indicator ends, inclusive. Equal to column when there is none.
	endColumn: number;
}

interface Window {
	runActionlint?(src: string): void;
	getYamlSource(): string;
	showError(msg: string): void;
	onCheckCompleted(errs: ActionlintError[]): void;
	dismissLoading(): void;
}

declare class Go {
	importObject: WebAssembly.Imports;
	run(mod: WebAssembly.Instance): Promise<unknown>;
}

declare module '*wasm_exec.js';
