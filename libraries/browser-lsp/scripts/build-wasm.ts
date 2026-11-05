import { spawnSync } from 'node:child_process';
import { copyFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../../..');
const output = resolve(import.meta.dirname, '../dist');
const built = spawnSync('go', /* dprint-ignore */ [
	'build',
	'-p=1',
	'-trimpath',
	'-o', join(output, 'server.wasm'),
	'./cmd/actionlint-lsp-wasm',
], {
	cwd: root,
	env: { ...process.env, GOOS: 'js', GOARCH: 'wasm', GOMAXPROCS: process.env.GOMAXPROCS ?? '2' },
	stdio: 'inherit',
});
if (built.error) throw built.error;
if (built.status !== 0) throw new Error('Could not compile the browser language server');
const goroot = spawnSync('go', ['env', 'GOROOT'], { cwd: root, encoding: 'utf8' });
if (goroot.error) throw goroot.error;
if (goroot.status !== 0) throw new Error('Could not locate the Go WebAssembly runtime');
await copyFile(join(goroot.stdout.trim(), 'lib/wasm/wasm_exec.js'), join(output, 'wasm_exec.js'));
