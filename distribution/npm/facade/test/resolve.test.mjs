import { platformPackage, resolveBinary } from '#resolve';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { after, before, describe, test } from 'node:test';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const facadeDir = join(here, '..');
const npmDir = join(facadeDir, '..');

/**
 * @typedef {object} Target
 * @property {string} pkg - npm name suffix, always `<os>-<cpu>`.
 * @property {string} os - Node `process.platform` value.
 * @property {string} cpu - Node `process.arch` value.
 */

/** @type {{ name: string, bin: Record<string, string>, files: string[], optionalDependencies: Record<string, string> }} */
const facade = JSON.parse(readFileSync(join(facadeDir, 'package.json'), 'utf8'));
/** @type {{ facade: string, binary: string, targets: Target[] }} */
const targets = JSON.parse(readFileSync(join(npmDir, 'targets.json'), 'utf8'));

const FACADE = facade.name;

/** Every platform package name the published facade would declare. */
const allPackages = targets.targets.map((t) => `${FACADE}-${t.pkg}`);

/**
 * A resolver context that pretends the named packages are installed, without
 * touching node_modules.
 *
 * @param {object} options
 * @param {string} options.platform
 * @param {string} options.arch
 * @param {readonly string[]} [options.packages] Declared in optionalDependencies.
 * @param {readonly string[]} [options.installed] Present on disk; defaults to all declared.
 * @param {readonly string[]} [options.missingBins] Installed but with no binary.
 */
function context({ platform, arch, packages = allPackages, installed, missingBins = [] }) {
	const present = new Set(installed ?? packages);
	return {
		platform,
		arch,
		packages,
		/** @param {string} pkg */
		resolvePackageJson(pkg) {
			if (!present.has(pkg)) {
				const err = new Error(`Cannot find module '${pkg}/package.json'`);
				// @ts-expect-error -- mirroring Node's resolution failure
				err.code = 'MODULE_NOT_FOUND';
				throw err;
			}
			return `/fake/node_modules/${pkg}/package.json`;
		},
		/** @param {string} path */
		fileExists: (path) => !missingBins.some((pkg) => path.startsWith(`/fake/node_modules/${pkg}/`)),
	};
}

// The failure paths deliberately print a diagnostic before throwing; keep the
// test output readable without losing the assertion that they threw.
/** @type {typeof console.error} */
let consoleError;
before(() => {
	consoleError = console.error;
	console.error = () => {};
});
after(() => {
	console.error = consoleError;
});

describe('platformPackage', () => {
	test('follows the <facade>-<os>-<cpu> convention', () => {
		assert.equal(platformPackage(FACADE, 'linux', 'x64'), '@kjanat/actionlint-linux-x64');
		assert.equal(platformPackage(FACADE, 'win32', 'arm64'), '@kjanat/actionlint-win32-arm64');
	});

	test('derives exactly the names targets.json declares', () => {
		for (const target of targets.targets) {
			assert.equal(
				platformPackage(FACADE, target.os, target.cpu),
				`${FACADE}-${target.pkg}`,
				`target ${target.pkg} must equal <os>-<cpu>, or the resolver cannot find it`,
			);
		}
	});
});

describe('resolveBinary', () => {
	test('resolves the package matching the host', () => {
		const path = resolveBinary('actionlint', context({ platform: 'linux', arch: 'x64' }));
		assert.equal(path, '/fake/node_modules/@kjanat/actionlint-linux-x64/bin/actionlint');
	});

	test('appends .exe on Windows only', () => {
		assert.ok(resolveBinary('actionlint', context({ platform: 'win32', arch: 'x64' })).endsWith('actionlint.exe'));
		assert.ok(resolveBinary('actionlint', context({ platform: 'darwin', arch: 'arm64' })).endsWith('actionlint'));
	});

	test('resolves every published target', () => {
		for (const target of targets.targets) {
			const path = resolveBinary('actionlint', context({ platform: target.os, arch: target.cpu }));
			assert.ok(
				path.includes(`${FACADE}-${target.pkg}/`),
				`${target.pkg} resolved to the wrong package: ${path}`,
			);
		}
	});

	// A static Go binary runs on musl too, so there is deliberately no separate
	// Alpine package to pick: linux-x64 is the answer on glibc and musl alike.
	test('uses one linux package regardless of libc', () => {
		const path = resolveBinary('actionlint', context({ platform: 'linux', arch: 'x64' }));
		assert.ok(path.includes('actionlint-linux-x64/'));
		assert.ok(!path.includes('musl') && !path.includes('gnu'));
	});

	test('throws when the platform has no published build', () => {
		assert.throws(
			() => resolveBinary('actionlint', context({ platform: 'sunos', arch: 'x64' })),
			/No published build for sunos-x64/,
		);
	});

	test('throws when the matching package was not installed', () => {
		assert.throws(
			() => resolveBinary('actionlint', context({ platform: 'linux', arch: 'x64', installed: [] })),
			/platform package is not installed/,
		);
	});

	// A half-unpacked install leaves package.json resolvable but no binary. The
	// resolver must report that, ahead of any ENOENT from spawnSync.
	test('throws when the package is present but its binary is not', () => {
		assert.throws(
			() =>
				resolveBinary(
					'actionlint',
					context({ platform: 'linux', arch: 'x64', missingBins: [`${FACADE}-linux-x64`] }),
				),
			/binary is missing/,
		);
	});
});

describe('facade manifest', () => {
	test('declares the binary the launcher asks for', () => {
		assert.deepEqual(Object.keys(facade.bin), ['actionlint']);
	});

	test('ships the files the bin entry points at', () => {
		assert.ok(facade.files.includes('bin/'));
		assert.ok(facade.files.includes('lib/'));
	});

	test('leaves optionalDependencies for the build to stamp', () => {
		assert.deepEqual(facade.optionalDependencies, {});
	});
});
