import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createHash } from 'node:crypto';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { temporary } from '#native';
import type { PluginCache, PluginPlan } from '#plugin-cache';
import { cachePlugins, inspectPlugins, parsePluginPlan, pluginCacheKey } from '#plugin-cache';
import { InputError } from '#runtime';

const bytes = Buffer.from('pinned wasm fixture');
const digest = createHash('sha256').update(bytes).digest('hex');

async function fixture(run: (plan: PluginPlan) => Promise<void>): Promise<void> {
	await temporary(async (root) => {
		const directory = join(root, 'actionlint', 'artifacts');
		await mkdir(directory, { recursive: true });
		await run({ directory, artifacts: [digest] });
	});
}

test('cold cache saves verified artifacts despite findings; warm cache avoids hydration and upload', async () => {
	await fixture(async (plan) => {
		const entries = new Map<string, Buffer>();
		const saves: string[] = [];
		const client: PluginCache = {
			restore: async (paths, key) => {
				assert.deepEqual(paths, [join(plan.directory, digest)]);
				const cached = entries.get(key);
				if (!cached) return undefined;
				await writeFile(join(plan.directory, digest), cached);
				return key;
			},
			save: async (paths, key) => {
				assert.deepEqual(paths, [join(plan.directory, digest)]);
				entries.set(key, await readFile(join(plan.directory, digest)));
				saves.push(key);
			},
		};
		// Unplanned files must never enter the archive.
		await writeFile(join(plan.directory, 'private-data'), 'unrelated');
		let downloads = 0;
		assert.equal(
			await cachePlugins(plan, async () => {
				downloads++;
				await writeFile(join(plan.directory, digest), bytes);
				return 1;
			}, client),
			1,
		);
		await rm(join(plan.directory, digest));
		assert.equal(
			await cachePlugins(plan, async () => {
				assert.deepEqual(await readFile(join(plan.directory, digest)), bytes);
				return 0;
			}, client),
			0,
		);
		assert.equal(downloads, 1);
		assert.deepEqual(saves, [pluginCacheKey(plan)]);
	});
});

test('pin changes get different keys; order and repeated digests do not', () => {
	const first = { directory: '', artifacts: [digest, 'a'.repeat(64)] };
	const second = { directory: '', artifacts: ['a'.repeat(64), digest, digest] };
	assert.equal(pluginCacheKey(first), pluginCacheKey(second));
	assert.notEqual(pluginCacheKey(first), pluginCacheKey({ ...first, artifacts: [digest] }));
	assert.notEqual(pluginCacheKey(first, 'linux'), pluginCacheKey(first, 'win32'));
});

test('first migration run saves newly pinned artifacts even when lint finds problems', async () => {
	await fixture(async (plan) => {
		const empty = { ...plan, artifacts: [] };
		let saved = false;
		assert.equal(
			await cachePlugins(empty, async () => {
				await writeFile(join(plan.directory, digest), bytes);
				return 1;
			}, {
				restore: async () => assert.fail('empty plan must not restore'),
				save: async (paths, key) => {
					assert.deepEqual(paths, [join(plan.directory, digest)]);
					assert.equal(key, pluginCacheKey(plan));
					saved = true;
				},
			}, async () => plan),
			1,
		);
		assert.equal(saved, true);
	});
});

test('post-analysis plan failure preserves findings and native failures', async (t) => {
	const warnings: string[] = [];
	t.mock.method(console, 'log', (message: string) => warnings.push(message));
	await fixture(async (plan) => {
		const empty = { ...plan, artifacts: [] };
		const client: PluginCache = {
			restore: async () => assert.fail('empty plan must not restore'),
			save: async () => assert.fail('unknown migrated artifacts must not be saved'),
		};
		const refresh = async (): Promise<PluginPlan> => {
			throw new Error('config moved after analysis');
		};
		assert.equal(await cachePlugins(empty, async () => 1, client, refresh), 1);
		await assert.rejects(
			cachePlugins(
				empty,
				async () => {
					throw new Error('native execution failed');
				},
				client,
				refresh,
			),
			/native execution failed/,
		);
	});
	assert.equal(warnings.length, 2);
	assert.ok(warnings.every((message) => message.includes('plan refresh failed')));
});

test('restore and save outages remain nonfatal', async (t) => {
	const warnings: string[] = [];
	t.mock.method(console, 'log', (message: string) => warnings.push(message));
	await fixture(async (plan) => {
		assert.equal(
			await cachePlugins(plan, async () => {
				await writeFile(join(plan.directory, digest), bytes);
				return 1;
			}, {
				restore: async () => {
					throw new Error('restore unavailable');
				},
				save: async () => {
					throw new Error('save unavailable');
				},
			}),
			1,
		);
	});
	assert.equal(warnings.length, 2);
	assert.match(warnings[0] ?? '', /restore failed/);
	assert.match(warnings[1] ?? '', /save failed/);
});

test('failed hydration never publishes an incomplete artifact set', async () => {
	await fixture(async (plan) => {
		const incomplete = { ...plan, artifacts: [digest, 'f'.repeat(64)] };
		await assert.rejects(
			cachePlugins(incomplete, async () => {
				await writeFile(join(plan.directory, digest), bytes);
				throw new Error('second download failed');
			}, {
				restore: async () => undefined,
				save: async () => assert.fail('incomplete cache must not be saved'),
			}),
			/second download failed/,
		);
	});
});

test('verified hydration is saved when later execution throws', async () => {
	await fixture(async (plan) => {
		let saved = false;
		await assert.rejects(
			cachePlugins(plan, async () => {
				await writeFile(join(plan.directory, digest), bytes);
				throw new Error('later failure');
			}, {
				restore: async () => undefined,
				save: async () => {
					saved = true;
				},
			}),
			/later failure/,
		);
		assert.equal(saved, true);
	});
});

test('corrupted restored bytes are fatal before execution', async () => {
	await fixture(async (plan) => {
		await assert.rejects(
			cachePlugins(plan, async () => assert.fail('must not execute corrupted artifact'), {
				restore: async (_paths, key) => {
					await writeFile(join(plan.directory, digest), 'corrupt');
					return key;
				},
				save: async () => assert.fail('corrupt cache must not be saved'),
			}),
			/SHA256 mismatch/,
		);
	});
});

test('directories cannot masquerade as artifact files', async () => {
	await fixture(async (plan) => {
		await mkdir(join(plan.directory, digest));
		await assert.rejects(
			cachePlugins(plan, async () => assert.fail('invalid artifact'), {
				restore: async () => undefined,
				save: async () => assert.fail('invalid artifact'),
			}),
			/not a regular file/,
		);
	});
});

test('empty artifact plans do not contact the cache service', async () => {
	await fixture(async (plan) => {
		assert.equal(
			await cachePlugins({ ...plan, artifacts: [] }, async () => 0, {
				restore: async () => assert.fail('empty cache restore'),
				save: async () => assert.fail('empty cache save'),
			}),
			0,
		);
	});
});

test('legal glob characters in cache directories preserve native execution', async () => {
	await temporary(async (root) => {
		const directory = join(root, 'runner[1]', 'actionlint', 'artifacts');
		const plan = await inspectPlugins('actionlint', {}, async () => ({
			exitCode: 0,
			stdout: JSON.stringify({ directory, artifacts: [digest] }),
			stderr: '',
		}));
		let executed = false;
		const client: PluginCache = {
			restore: async () => assert.fail('glob-bearing path must not reach toolkit restore'),
			save: async () => assert.fail('glob-bearing path must not reach toolkit save'),
		};
		assert.equal(
			await cachePlugins(plan, async () => {
				executed = true;
				await mkdir(directory, { recursive: true });
				await writeFile(join(directory, digest), bytes);
				return 1;
			}, client),
			1,
		);
		assert.equal(executed, true);
		await assert.rejects(
			cachePlugins(plan, async () => {
				throw new Error('native failure');
			}, client),
			/native failure/,
		);
	});
});

test('a refreshed glob-bearing cache directory is never uploaded', async () => {
	await fixture(async (plan) => {
		let executed = false;
		assert.equal(
			await cachePlugins({ ...plan, artifacts: [] }, async () => {
				executed = true;
				return 1;
			}, {
				restore: async () => assert.fail('empty plan must not restore'),
				save: async () => assert.fail('refreshed glob-bearing path must not be saved'),
			}, async () => ({ ...plan, directory: join(plan.directory, '[new]', 'actionlint', 'artifacts') })),
			1,
		);
		assert.equal(executed, true);
	});
});

test('cache plans reject path traversal, glob expansion and malformed digests', async () => {
	await fixture(async (plan) => {
		for (
			const directory of ['', '.', '/', join(plan.directory, '..'), join(plan.directory, '*'), `${plan.directory}\n`]
		) {
			assert.throws(() => parsePluginPlan({ ...plan, directory }), /cache directory/);
		}
		for (const bad of ['../private', '*', 'A'.repeat(64), digest.slice(1), 42]) {
			assert.throws(() => parsePluginPlan({ ...plan, artifacts: [bad] }), /artifact digest/);
		}
		for (const value of [null, {}, { directory: plan.directory, artifacts: null }]) {
			assert.throws(() => parsePluginPlan(value), /cache plan/);
		}
		assert.deepEqual(parsePluginPlan({ ...plan, artifacts: [digest, digest] }), plan);
	});
});

test('preflight parses JSON and rejects unsupported binaries and invalid configuration', async () => {
	await fixture(async (plan) => {
		assert.deepEqual(
			await inspectPlugins('actionlint', {}, async (_executable, args) => {
				assert.deepEqual(args, ['-github-action-plugins']);
				return { exitCode: 0, stdout: JSON.stringify(plan), stderr: '' };
			}),
			plan,
		);
		for (
			const stderr of [
				'flag provided but not defined: -github-action-plugins',
				'unknown flag: --github-action-plugins',
			]
		) {
			await assert.rejects(
				inspectPlugins('actionlint', {}, async () => ({ exitCode: 2, stdout: '', stderr })),
				InputError,
			);
		}
		await assert.rejects(
			inspectPlugins('actionlint', {}, async () => ({
				exitCode: 2,
				stdout: '',
				stderr: 'invalid manifest',
			})),
			InputError,
		);
		await assert.rejects(
			inspectPlugins('actionlint', {}, async () => ({
				exitCode: 3,
				stdout: '',
				stderr: 'read failed',
			})),
			/read failed/,
		);
		await assert.rejects(
			inspectPlugins('actionlint', {}, async () => ({
				exitCode: 2,
				stdout: '',
				stderr: 'unknown flag: --github-action-plugins-unrelated',
			})),
			InputError,
		);
	});
});
