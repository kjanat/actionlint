// This is the CLI subset emitted by actionlint's embedded ShellCheck rule.
// Refuse new flags until their semantics are mapped deliberately.
export function shellcheckRequest(args: string[], script: string): ShellcheckRequest {
	const request: ShellcheckRequest = { script, filename: '-', exclude: [] };
	for (let i = 0; i < args.length; i++) {
		const flag = args[i];
		switch (flag) {
			case '--norc':
			case '-':
				break;
			case '-f':
				if (args[++i] !== 'json1') throw new Error('ShellCheck requires json1 output');
				break;
			case '--shell': {
				const shell = args[++i];
				if (!shell || !['bash', 'sh', 'dash', 'ksh', 'busybox'].includes(shell)) {
					throw new Error(`Unsupported ShellCheck dialect: ${shell}`);
				}
				request.shell = shell;
				break;
			}
			case '-e': {
				const codes = args[++i];
				if (!codes || !/^SC\d+(,SC\d+)*$/.test(codes)) throw new Error('Invalid ShellCheck exclusions');
				request.exclude = codes.split(',').map(code => Number(code.slice(2)));
				break;
			}
			default:
				throw new Error(`Unsupported embedded ShellCheck option: ${flag}`);
		}
	}
	return request;
}

export interface ShellcheckRequest {
	script: string;
	filename: string;
	shell?: string;
	exclude: number[];
}

export function shellcheckOutput(result: unknown): string {
	if (typeof result !== 'object' || result === null || !('comments' in result) || !Array.isArray(result.comments)) {
		throw new Error('Invalid ShellCheck result');
	}
	if ('dataflowError' in result && result.dataflowError !== null && result.dataflowError !== undefined) {
		throw new Error(`ShellCheck analysis incomplete: ${JSON.stringify(result.dataflowError)}`);
	}
	return JSON.stringify(result);
}
