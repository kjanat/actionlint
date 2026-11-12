/**
 * Query commands return captured stdout. With live output, commands inherit the
 * parent's output streams and return an empty buffer. Both modes wait for the child to exit.
 * @typedef {(program: string, args: string[], options?: {live?: boolean, timeout?: number}) => Promise<Buffer>} ExecuteCommand
 */

/** @param {Uint8Array} bytes */
export function wslDistributions(bytes) {
	const utf16 = bytes.includes(0) || (bytes[0] === 0xff && bytes[1] === 0xfe);
	if (utf16 && bytes.length % 2) throw new Error('Incomplete UTF-16 distribution names from WSL');
	const text = new TextDecoder(utf16 ? 'utf-16le' : 'utf-8').decode(bytes);
	return text.replace(/^\ufeff/, '').split('\n').map(line => line.trim()).filter(Boolean);
}

/** @param {{program?: string, args: string[], platform: string}} options @param {ExecuteCommand} execute */
export async function findNix(options, execute) {
	if (options.args.length && !options.program) throw new Error('--nix-arg requires --nix-program');
	if (options.program) {
		await execute(options.program, [...options.args, '--version'], { timeout: 15_000 });
		return { program: options.program, args: options.args };
	}
	try {
		await execute('nix', ['--version'], { timeout: 15_000 });
		return { program: 'nix', args: [] };
	} catch (error) {
		if (error instanceof Error && error.name === 'AbortError') throw error;
	}
	if (options.platform !== 'win32') {
		throw new Error('Release verification requires Nix; install it or select --nix-program');
	}
	let listed;
	try {
		listed = await execute('wsl.exe', ['--list', '--quiet'], { timeout: 15_000 });
	} catch (error) {
		if (error instanceof Error && error.name === 'AbortError') throw error;
		throw new Error('Nix was not found and WSL could not list installed distributions', { cause: error });
	}
	for (const distribution of wslDistributions(listed)) {
		const args = ['--distribution', distribution, '--exec', 'sh', '-lc', 'exec nix "$@"', 'nix'];
		try {
			await execute('wsl.exe', [...args, '--version'], { timeout: 15_000 });
			return { program: 'wsl.exe', args };
		} catch (error) {
			if (error instanceof Error && error.name === 'AbortError') throw error;
		}
	}
	throw new Error('No installed WSL distribution could run Nix; install Nix in WSL or select --nix-program');
}
