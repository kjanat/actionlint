import { existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { arch, platform } from 'node:process';

import { cyan, link, red, yellow } from 'ansispeck';

const require = createRequire(import.meta.url);
const { bugs, name: pkgName, optionalDependencies } = require('#pkg');

const issues = bugs.url;
const declared = Object.keys(optionalDependencies || {});

/**
 * A locked-down root can raise on a stat.
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
 * The release binaries are built with `CGO_ENABLED=0`. One `linux-<cpu>`
 * package covers glibc and musl hosts.
 *
 * @param {string} scope - npm scope the platform packages are published under.
 * @param {string} binary - Base name of the executable.
 * @param {string} os - Node `platform` string.
 * @param {string} cpu - Node `arch` string.
 * @returns {string}
 */
export function platformPackage(scope, binary, os, cpu) {
	return `${scope}/${binary}-${os}-${cpu}`;
}

/**
 * @param {readonly string[]} packages - Declared platform packages.
 * @param {string} binary - Base name of the executable.
 * @param {string} os
 * @param {string} cpu
 * @returns {string | undefined}
 */
function declaredPackage(packages, binary, os, cpu) {
	const suffix = `/${binary}-${os}-${cpu}`;
	return packages.find((pkg) => pkg.endsWith(suffix));
}

/**
 * @param {string} os
 * @param {string} cpu
 * @param {readonly string[]} packages
 * @returns {never}
 */
function failUnsupported(os, cpu, packages) {
	const indent = '  ';
	const supported = packages
		.map((pkg) => pkg.slice(pkg.indexOf('/') + 1).replace(/^[^-]+-/, ''))
		.sort()
		.join(', ');

	console.error(
		`${red(pkgName)}: no build is published for ${yellow(`${os}-${cpu}`)}.

Published platforms: ${cyan(supported)}

Workarounds:
${indent}- build from source, which works anywhere Go does: ${
	cyan('go install actionlint.kjanat.dev/v2/cmd/actionlint@latest')
}
${indent}- ask for this platform: ${link(issues, issues)}
`,
	);

	throw new Error(`No published build for ${os}-${cpu}.`);
}

/**
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
${indent}- bun + ${cyan('minimumReleaseAge')}: add ${cyan(`${wanted.slice(0, wanted.indexOf('/'))}/*`)} alongside ${
	cyan(pkgName)
} in ${cyan('minimumReleaseAgeExcludes')}; a fresh release is otherwise age-gated
${indent}- build from source instead: ${cyan('go install actionlint.kjanat.dev/v2/cmd/actionlint@latest')}
${indent}- file an issue: ${link(issues, issues)}
`,
	);

	throw new Error(`${summary} (${wanted})`);
}

/**
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

	const wanted = declaredPackage(packages, name, hostPlatform, hostArch);
	if (!wanted) failUnsupported(hostPlatform, hostArch, packages);

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
	if (!fileExists(binPath)) {
		failNotInstalled(
			wanted,
			`installed, but its binary is missing at ${binPath}`,
			'The platform package is installed but its binary is missing',
		);
	}

	return binPath;
}
