import { cyan, link, red, yellow } from 'ansispeck';
import { existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { arch, platform } from 'node:process';

const require = createRequire(import.meta.url);
const { bugs, name: pkgName, optionalDependencies } = require('#pkg');

const issues = bugs.url;
const declared = Object.keys(optionalDependencies || {});

/**
 * `existsSync` that never throws; a locked-down root can raise on a stat.
 *
 * @param {string} path
 * @returns {boolean}
 */
function exists(path) {
	try {
		return existsSync(path);
	} catch {
		return false;
	}
}

/**
 * Name of the platform package holding the build for a host.
 *
 * The `<facade>-<os>-<cpu>` convention is the contract, so the name is derived
 * rather than searched for. Resolution then never depends on the order
 * `optionalDependencies` happens to be generated in.
 *
 * Note there is no libc dimension. The release binaries are built with
 * `CGO_ENABLED=0`, so they are statically linked and the same `linux-<cpu>`
 * package runs on glibc and musl hosts alike — Alpine included. That is the one
 * way this resolver is simpler than the equivalent for a Rust binary, which has
 * to detect the host libc and pick between a gnu and a musl build.
 *
 * @param {string} facade - npm name of the facade package.
 * @param {string} os - Node `platform` string.
 * @param {string} cpu - Node `arch` string.
 * @returns {string}
 */
export function platformPackage(facade, os, cpu) {
	return `${facade}-${os}-${cpu}`;
}

/**
 * Report that this platform has no build at all, then throw.
 *
 * @param {string} os
 * @param {string} cpu
 * @param {readonly string[]} packages
 * @returns {never}
 */
function failUnsupported(os, cpu, packages) {
	const indent = '  ';
	const supported = packages
		.map((pkg) => pkg.slice(`${pkgName}-`.length))
		.sort()
		.join(', ');

	console.error(
		`${red(pkgName)}: no build is published for ${yellow(`${os}-${cpu}`)}.

Published platforms: ${cyan(supported)}

Workarounds:
${indent}- build from source, which works anywhere Go does: ${cyan('go install actionlint.kjanat.dev/cmd/actionlint@latest')}
${indent}- ask for this platform: ${link(issues, issues)}
`,
	);

	throw new Error(`No published build for ${os}-${cpu}.`);
}

/**
 * Report that the matching platform package is unusable, then throw.
 *
 * The two ways it can be unusable need different fixes — install it, versus
 * reinstall it — so `summary` distinguishes them in the thrown error too, not
 * only in the diagnostic block. The thrown message is what ends up in a stack
 * trace or a CI log, where the printed block above it may be lost.
 *
 * @param {string} wanted
 * @param {string} detail - Why it is unusable, shown in the diagnostic.
 * @param {string} summary - One-line reason carried by the thrown error.
 * @returns {never}
 */
function failNotInstalled(wanted, detail, summary) {
	const indent = '  ';

	console.error(
		`${red(pkgName)}: the platform package for this host is not usable.

Expected package: ${cyan(wanted)}
${indent}- ${detail}

This usually means your package manager skipped ${cyan('optionalDependencies')}
(common with ${cyan('--no-optional')}, ${cyan('--omit=optional')}, or some Docker and CI setups).

Workarounds:
${indent}- reinstall without ${cyan('--no-optional')} / ${cyan('--omit=optional')}
${indent}- install it explicitly: ${cyan(`npm install ${wanted}`)}
${indent}- bun + ${cyan('minimumReleaseAge')}: add ${cyan(`${pkgName}-*`)} (not just ${cyan(pkgName)}) to ${
			cyan('minimumReleaseAgeExcludes')
		}; a fresh release is otherwise age-gated
${indent}- build from source instead: ${cyan('go install actionlint.kjanat.dev/cmd/actionlint@latest')}
${indent}- file an issue: ${link(issues, issues)}
`,
	);

	throw new Error(`${summary} (${wanted})`);
}

/**
 * Locate the prebuilt executable matching the current platform and architecture.
 *
 * @param {string} name - Base name of the executable, without any extension.
 * @param {object} [context] - Host details to resolve against; defaults to this process.
 * @param {string} [context.platform] - Node `platform` string.
 * @param {string} [context.arch] - Node `arch` string.
 * @param {readonly string[]} [context.packages] - Declared platform packages.
 * @param {(pkg: string) => string} [context.resolvePackageJson]
 * @param {(path: string) => boolean} [context.fileExists]
 * @returns {string} Filesystem path to the executable.
 * @throws {Error} If no usable binary is installed for this host.
 */
export function resolveBinary(name, context = {}) {
	const {
		platform: hostPlatform = platform,
		arch: hostArch = arch,
		packages = declared,
		resolvePackageJson = (pkg) => require.resolve(`${pkg}/package.json`),
		fileExists = exists,
	} = context;

	const wanted = platformPackage(pkgName, hostPlatform, hostArch);
	// Absent from the manifest means no build exists for this host at all,
	// which is a different problem from one that exists but was not installed.
	if (!packages.includes(wanted)) failUnsupported(hostPlatform, hostArch, packages);

	let pkgJsonPath;
	try {
		pkgJsonPath = resolvePackageJson(wanted);
	} catch (err) {
		failNotInstalled(
			wanted,
			`not installed (${err instanceof Error ? err.message.split('\n')[0] : String(err)})`,
			'The platform package is not installed',
		);
	}

	const exe = hostPlatform === 'win32' ? `${name}.exe` : name;
	const binPath = join(dirname(pkgJsonPath), 'bin', exe);
	// Resolving package.json proves the package exists, not the binary. They can
	// disagree when an install half-succeeded or a bin was deleted by hand.
	// Prefer a clear error here over an opaque ENOENT from spawnSync later.
	if (!fileExists(binPath)) {
		failNotInstalled(
			wanted,
			`installed, but its binary is missing at ${binPath}`,
			'The platform package is installed but its binary is missing',
		);
	}

	return binPath;
}
