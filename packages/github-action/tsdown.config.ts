import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFile, realpath, writeFile } from 'node:fs/promises';
import { isBuiltin } from 'node:module';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';

import type { TsdownHooks, UserConfig } from 'tsdown';
import { defineConfig } from 'tsdown';

const packageDirectory = import.meta.dirname;
const repositoryDirectory = resolve(packageDirectory, '../..');

async function resolvedDestination(path: string): Promise<string> {
	try {
		return await realpath(path);
	} catch (error) {
		if (!(error instanceof Error) || !('code' in error) || error.code !== 'ENOENT') throw error;
		const parent = dirname(path);
		if (parent === path) throw error;
		return join(await resolvedDestination(parent), relative(parent, path));
	}
}

const writeReleaseChecksum: TsdownHooks['build:done'] = async ({ chunks, options }) => {
	const [chunk] = chunks;
	if (chunks.length !== 1 || !chunk || chunk.type !== 'chunk' || chunk.fileName !== 'action.mjs') {
		throw new Error(`Expected only action.mjs; emitted: ${chunks.map((chunk) => chunk.fileName).join(', ')}`);
	}
	const external = [...chunk.imports, ...chunk.dynamicImports].filter((name) => !isBuiltin(name));
	if (external.length > 0) throw new Error(`Unbundled runtime imports: ${external.join(', ')}`);
	const bundle = join(options.outDir, chunk.fileName);
	const digest = createHash('sha256').update(await readFile(bundle)).digest('hex');
	await writeFile(join(options.outDir, 'SHA256SUMS'), `${digest}  action.mjs\n`);
};

export default defineConfig(async ({ outDir }): Promise<UserConfig> => {
	if (!outDir || !isAbsolute(outDir)) {
		throw new Error('--out-dir must be an absolute artifact directory outside the source checkout');
	}
	const output = await resolvedDestination(outDir);
	const path = relative(await realpath(repositoryDirectory), output);
	if (path === '' || (path !== '..' && !path.startsWith(`..${sep}`) && !isAbsolute(path))) {
		throw new Error('Refusing to create the bundle inside the source checkout; choose an external --out-dir');
	}
	const version = process.env.ACTIONLINT_VERSION || execFileSync(
		'git',
		['describe', '--tags', '--abbrev=0', '--match', 'v[0-9]*.[0-9]*.[0-9]*', '--exclude', '*-*'],
		{ cwd: repositoryDirectory, encoding: 'utf8' },
	).trim().replace(/^v/, '');
	return {
		cwd: packageDirectory,
		entry: { action: 'src/main.ts' },
		outDir: output,
		format: 'esm',
		platform: 'node',
		target: 'node24',
		minify: true,
		treeshake: true,
		dts: false,
		sourcemap: false,
		clean: false,
		failOnWarn: true,
		env: { NODE_ENV: 'production' },
		define: {
			__ACTIONLINT_VERSION__: JSON.stringify(version),
			__PYFLAKES_LAUNCHER__: JSON.stringify(await readFile(new URL('./tools/pyflakes.py', import.meta.url), 'utf8')),
		},
		deps: { onlyImport: [] },
		outputOptions: {
			entryFileNames: '[name].mjs',
			codeSplitting: false,
			comments: { legal: true, annotation: false, jsdoc: false },
		},
		hooks: { 'build:done': writeReleaseChecksum },
	};
});
