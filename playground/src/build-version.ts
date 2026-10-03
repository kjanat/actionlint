import { execFileSync } from 'node:child_process';

const repository = 'https://github.com/kjanat/actionlint';
const stableTag = /^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/;

export function buildVersion(directory: string, latestRelease = '') {
	if (latestRelease && (!stableTag.test(latestRelease) || latestRelease.trim() !== latestRelease)) {
		throw new Error('Invalid latest published release tag');
	}
	const release = latestRelease
		? { version: latestRelease, url: `${repository}/releases/tag/${latestRelease}` }
		: undefined;
	try {
		const git = (...args: string[]) =>
			execFileSync('git', args, { cwd: directory, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
		const ref = git('rev-parse', 'HEAD');
		const version = git('describe', '--tags', '--match', 'v[0-9]*.[0-9]*.[0-9]*', '--always', '--dirty');
		const url = stableTag.test(version) ? `${repository}/releases/tag/${version}` : `${repository}/tree/${ref}`;
		return { version, ref, url, release };
	} catch {
		return { version: 'development', ref: 'HEAD', url: repository, release };
	}
}

export function renderBuildVersion(html: string, identity: ReturnType<typeof buildVersion>): string {
	const release = identity.release;
	const releaseLink = release
		? `<a rel="noopener" href="${release.url}"><span class="tag is-info">Latest release ${release.version}</span></a>`
		: '';
	return html.replaceAll('%ACTIONLINT_VERSION%', identity.version)
		.replaceAll('%ACTIONLINT_VERSION_URL%', identity.url)
		.replaceAll('%ACTIONLINT_REF%', identity.ref)
		.replaceAll('%ACTIONLINT_LATEST_RELEASE%', releaseLink);
}
