import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const systems = ['x86_64-linux', 'aarch64-linux', 'aarch64-darwin'];

/**
 * @param {string} version
 * @param {{root?: string, run?: (command: string, args: string[]) => string}} options
 */
export function updateToolchain(version, { root = process.cwd(), run } = {}) {
	const match = /^go(\d+)\.(\d+)\.(\d+)$/.exec(version);
	if (!match) throw new Error(`Invalid Go toolchain: ${version}`);
	const expected = version.slice(2);
	const attribute = `go_${match[1]}_${match[2]}`;
	const compilerPath = `pkgs/development/compilers/go/${match[1]}.${match[2]}.nix`;
	run ??= (command, args) => execFileSync(command, args, { cwd: root, encoding: 'utf8' }).trim();
	const read = (/** @type {string} */ name) => readFileSync(resolve(root, name), 'utf8');
	const lock = JSON.parse(read('flake.lock'));
	const current = lock.nodes.nixpkgs.locked.rev;
	const revisions = [current];
	let revision;
	for (let i = 0; i < revisions.length; i++) {
		const candidate = revisions[i];
		if (!/^[a-f0-9]{40}$/.test(candidate)) throw new Error('Invalid nixpkgs commit ID');
		let source = '';
		try {
			source = run('gh', [
				'api',
				`repos/NixOS/nixpkgs/contents/${compilerPath}?ref=${candidate}`,
				'-H',
				'Accept: application/vnd.github.raw+json',
			]);
		} catch {
			// A new Go family can be absent from the currently locked revision.
		}
		if (source.match(/\bversion\s*=\s*"([^"]+)"\s*;/)?.[1] === expected) {
			for (const system of systems) {
				const actual = run('nix', [
					'eval',
					'--raw',
					`github:NixOS/nixpkgs/${candidate}#legacyPackages.${system}.${attribute}.version`,
				]);
				if (actual !== expected) {
					throw new Error(`nixpkgs ${candidate}: ${system} provides Go ${actual}, expected ${expected}`);
				}
			}
			revision = candidate;
			break;
		}
		if (i === 0) {
			const commits = run('gh', [
				'api',
				`repos/NixOS/nixpkgs/commits?path=${compilerPath}&per_page=100`,
				'--jq',
				'.[].sha',
			]);
			revisions.push(...commits.split('\n').filter(sha => sha && sha !== current));
		}
	}
	if (!revision) throw new Error(`No nixpkgs compiler provides ${version}; refusing a partial toolchain bump`);

	// Validate every replacement before touching the repository.
	const replacements = (/** @type {[string, RegExp, string][]} */ ([
		['go.mod', /^toolchain go\d+\.\d+\.\d+$/m, `toolchain ${version}`],
		['Dockerfile', /^ARG GOLANG_VER=\d+\.\d+\.\d+$/m, `ARG GOLANG_VER=${expected}`],
		['CONTRIBUTING.md', /--build-arg GOLANG_VER=\d+\.\d+\.\d+/g, `--build-arg GOLANG_VER=${expected}`],
		[
			'flake.nix',
			/inputs\.nixpkgs\.url = "github:NixOS\/nixpkgs\/[^"]+";/,
			`inputs.nixpkgs.url = "github:NixOS/nixpkgs/${revision}";`,
		],
	])).map(([name, pattern, replacement]) => {
		const before = read(name);
		if (!pattern.test(before)) throw new Error(`Missing toolchain setting in ${name}`);
		return [name, before.replace(pattern, replacement)];
	});
	for (const [name, content] of replacements) writeFileSync(resolve(root, name), content);
	run('nix', ['flake', 'lock']);
	const updated = JSON.parse(read('flake.lock'));
	if (updated.nodes.nixpkgs.locked.rev !== revision) throw new Error('Nix locked an unexpected compiler revision');
	run('nix', ['flake', 'check', '--all-systems', '--no-build', '--no-update-lock-file']);
	return revision;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
	try {
		updateToolchain(process.argv[2] ?? '');
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
	}
}
