/** @type {import('#release/version-references.mjs').VersionTarget[]} */
export const targets = [
	{
		path: 'flake.nix',
		rules: [
			{ description: 'Nix package version', pattern: /^ {6}version = "(\d+\.\d+\.\d+)";\r?$/dgm, count: 1 },
		],
		unrelated: [
			'nixpkgs merge that adds Go 1.27.4',
		],
	},
	{
		path: 'distribution/install/download-actionlint.bash',
		rules: [
			{ description: 'default version of the download script', pattern: /^version="(\d+\.\d+\.\d+)"\r?$/dgm, count: 1 },
		],
		unrelated: [
			'1.6.9',
			'releases/download/v1.0.0/actionlint_1.0.0_linux_386.tar.gz',
		],
	},
	{
		path: 'docs/usage.md',
		rules: [
			{ description: 'download script argument', pattern: /download-actionlint\.bash\) (\d+\.\d+\.\d+)/dgm, count: 2 },
			{ description: 'CLI image tag example', pattern: /`ghcr\.io\/kjanat\/actionlint:(\d+\.\d+\.\d+)`/dgm, count: 1 },
			{ description: 'Trunk linter version', pattern: / actionlint@(\d+\.\d+\.\d+)/dgm, count: 2 },
		],
		unrelated: [
			'sarif/v2.1.0/sarif-v2.1.0.html',
			'git-scm.com/docs/git-hook/2.54.0',
			'git-scm.com/docs/git-hook/2.55.0',
			'We recommend v1.76.2 or newer.',
		],
	},
	{
		path: 'docs/install.md',
		rules: [
			{ description: 'gh release download tag', pattern: /--pattern '[^']+' v(\d+\.\d+\.\d+)/dgm, count: 1 },
			{ description: 'release asset name', pattern: /actionlint_(\d+\.\d+\.\d+)_linux_amd64\.tar\.gz/dgm, count: 2 },
			{ description: 'download script example description', pattern: /example installs v(\d+\.\d+\.\d+)/dgm, count: 1 },
			{ description: 'download script argument', pattern: /download-actionlint\.bash\) (\d+\.\d+\.\d+)/dgm, count: 1 },
		],
		unrelated: [
			'has proposed switching the package to this fork at v1.16.0',
			'mise 2026.9.7 release PR',
			'On mise 2026.9.6,',
		],
	},
	{
		path: 'man/actionlint.1.md',
		rules: [
			{ description: 'manual footer version', pattern: /^footer: actionlint (\d+\.\d+\.\d+)\r?$/dgm, count: 1 },
			{ description: 'document link', pattern: /\/blob\/v(\d+\.\d+\.\d+)\/docs\//dgm, count: 5 },
		],
		generated: [
			/kjanat:actionlint\/(?:analyzer|runner-provider)@\d+\.\d+\.\d+/gm,
		],
	},
	{
		path: '.github/workflows/npm-release.yml',
		rules: [
			{
				description: 'npm workflow release tag example',
				pattern: /Release tag to publish \(e\.g\. v(\d+\.\d+\.\d+)\)/dgm,
				count: 1,
			},
		],
		generated: [
			/uses: [\w.-]+\/[\w./-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+\r?$/dgm,
			/^\s+version: \d+\.\d+\.\d+,?\r?$/dgm,
		],
	},
	{
		path: '.github/workflows/pypi-release.yml',
		rules: [
			{
				description: 'PyPI workflow release tag example',
				pattern: /Release tag to publish \(e\.g\. v(\d+\.\d+\.\d+)\)/dgm,
				count: 1,
			},
		],
		generated: [
			/uses: [\w.-]+\/[\w./-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+\r?$/dgm,
		],
	},
	{
		path: 'distribution/npm/README.md',
		rules: [
			{ description: 'npm package build version', pattern: /INPUT_VERSION=(\d+\.\d+\.\d+)/dgm, count: 1 },
		],
		unrelated: [
			'0.0.0',
		],
	},
	{
		path: 'distribution/aur/actionlint-kjanat-git/PKGBUILD',
		rules: [
			{
				description: 'AUR git package base version',
				pattern: /^pkgver=(\d+\.\d+\.\d+)\.r0\.g0000000\r?$/dgm,
				count: 1,
			},
			{ description: 'AUR git describe example', pattern: /# v(\d+\.\d+\.\d+)-12-gabc1234/dgm, count: 1 },
			{ description: 'AUR pkgver example', pattern: /-> (\d+\.\d+\.\d+)\.r12\.gabc1234/dgm, count: 1 },
		],
	},
];
