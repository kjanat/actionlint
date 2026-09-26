import type { Environment } from '#runtime';

// Normalize before adding overrides: Windows treats every environment key alike,
// but plain JavaScript objects and Node's duplicate-key selection do not.
export function normalizeEnvironment(
	environment: NodeJS.ProcessEnv,
	platform: NodeJS.Platform = process.platform,
): Environment {
	const result = new Map<string, string>();
	for (const [key, value] of Object.entries(environment)) {
		if (value !== undefined) result.set(platform === 'win32' ? key.toUpperCase() : key, value);
	}
	return Object.fromEntries(result);
}

export function subprocessEnvironment(environment: NodeJS.ProcessEnv): Environment {
	const child = normalizeEnvironment(environment);
	delete child.INPUT_TOKEN;
	return child;
}
