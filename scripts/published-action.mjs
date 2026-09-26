import { mkdir, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

/** Build a local composite whose literal external references exercise runner resolution.
 * @param {string} repository @param {string} version @param {string} commit
 */
export function publishedAction(repository, version, commit) {
	if (repository.trim() !== repository || !/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid repository');
	if (version.trim() !== version || !/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(version)) {
		throw new Error('Invalid stable version');
	}
	if (commit.trim() !== commit || !/^[a-f0-9]{40}$/.test(commit)) throw new Error('Invalid candidate commit');
	const pins = [{ id: 'version', ref: `v${version}` }, { id: 'commit', ref: commit }];
	return {
		name: 'Check published Action pins',
		description: 'Resolve the published version and candidate SHA, then verify both runs.',
		runs: {
			using: 'composite',
			steps: pins.flatMap(({ id, ref }) => [
				{
					id,
					uses: `${repository}@${ref}`,
					env: { ACTIONLINT_ACTION_BINARY: '' },
					with: { files: 'testdata/ok/minimal.yaml', format: 'json', shellcheck: 'false', pyflakes: 'false' },
				},
				{
					name: `Verify ${id} outputs`,
					shell: 'bash',
					env: {
						EXIT_CODE: `\${{ steps.${id}.outputs.exit-code }}`,
						RESULT: `\${{ steps.${id}.outputs.result }}`,
						PROBLEM_COUNT: `\${{ steps.${id}.outputs.problem-count }}`,
						OUTPUT: `\${{ steps.${id}.outputs.output }}`,
					},
					run: 'test "$EXIT_CODE" = 0\ntest "$RESULT" = success\ntest "$PROBLEM_COUNT" = 0\ntest "$OUTPUT" = \'[]\'\n',
				},
			]),
		},
	};
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
	const [repository, version, commit, directory, ...extra] = process.argv.slice(2);
	if (!repository || !version || !commit || !directory || extra.length) {
		throw new Error('Usage: published-action.mjs REPOSITORY VERSION COMMIT DIRECTORY');
	}
	const action = publishedAction(repository, version, commit);
	await mkdir(directory, { recursive: true });
	// JSON is valid YAML and keeps values literal without hand-written escaping.
	await writeFile(join(directory, 'action.yml'), `${JSON.stringify(action, null, 2)}\n`, { flag: 'wx' });
}
