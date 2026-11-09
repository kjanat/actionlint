import { readFileSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { packageRepositoryUrl } from 'dreamcli/config';

import pkg from '#pkg' with { type: 'json' };

export function goMetadata(repositoryRoot: string) {
	const goMod = readFileSync(join(repositoryRoot, 'go.mod'), 'utf8');
	const modulePath = /^module\s+(\S+)\s*$/mu.exec(goMod)?.[1];
	if (modulePath === undefined) throw new Error('go.mod has no module path');
	const version = /\/(v(?:[2-9]|[1-9][0-9]+))$/u.exec(modulePath)?.[1];
	if (version === undefined) throw new Error(`Go module path has no major version suffix: ${modulePath}`);
	const root = modulePath.slice(0, -(version.length + 1));
	const repository = packageRepositoryUrl(pkg, { require: true });
	return { root, modulePath, version, repository };
}

export function renderGoMetadata(html: string, modulePath: string, repository: string): string {
	const repo = repository;
	const tags =
		`<meta name="go-import" content="${modulePath} git ${repo}">\n\t\t<meta name="go-source" content="${modulePath} ${repo} ${repo}/tree/HEAD{/dir} ${repo}/blob/HEAD{/dir}/{file}#L{line}">`;
	return html.replace('%GO_IMPORT_METADATA%', tags);
}

export async function writeGoImportPage(outDir: string, metadata: ReturnType<typeof goMetadata>): Promise<void> {
	const directory = join(outDir, metadata.version);
	await mkdir(directory, { recursive: true });
	const html = renderGoMetadata(
		`<!DOCTYPE html>\n<html lang="en">\n\t<head>\n\t\t<meta charset="utf-8">\n\t\t%GO_IMPORT_METADATA%\n\t\t<title>actionlint Go module</title>\n\t</head>\n\t<body><a href="../">actionlint playground</a></body>\n</html>\n`,
		metadata.modulePath,
		metadata.repository,
	);
	await writeFile(join(directory, 'index.html'), html);
}
