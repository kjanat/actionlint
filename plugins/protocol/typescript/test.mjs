import { execFileSync } from 'node:child_process';
import { rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('.', import.meta.url));
const jco = fileURLToPath(new URL('node_modules/@bytecodealliance/jco/dist/jco.js', import.meta.url));
const tsc = fileURLToPath(new URL('node_modules/typescript/bin/tsc', import.meta.url));
const options = ['--nominal-resources', '--typed-error-classes', '--separate-interface-types'];
const run = (file, args) => execFileSync(process.execPath, [file, ...args], { cwd: root, stdio: 'inherit' });

rmSync(new URL('.generated/', import.meta.url), { recursive: true, force: true });
for (
	const [world, name] of [
		['kjanat:actionlint/plugin@0.2.0', 'plugin'],
		['kjanat:actionlint/runner-plugin@0.2.0', 'runner-02'],
		['kjanat:actionlint/runner-plugin@0.3.0', 'runner-03'],
	]
) {
	run(jco, [
		'types',
		'../wit',
		'--world-name',
		world,
		'--name',
		name,
		'-o',
		`.generated/${name}`,
		...options,
		'--quiet',
	]);
}
run(jco, [
	'types',
	'identity.wit',
	'--world-name',
	'plugin',
	'--name',
	'identity',
	'-o',
	'.generated/identity',
	...options,
	'--quiet',
]);
run(tsc, ['--project', 'tsconfig.json']);
console.log('Component declarations and consumer contracts passed.');
