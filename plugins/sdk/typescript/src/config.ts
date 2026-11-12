import type { JSONValue } from '#index';

/**
 * Require an object-shaped plugin config before reading named options.
 * @example
 * ```ts
 * import { configObject, stringOption } from '@kjanat-actionlint/plugin-sdk/config';
 * const settings = configObject(request.config);
 * const mode = stringOption(settings, 'mode', ['strict', 'relaxed']) ?? 'strict';
 * ```
 */
export function configObject(config: JSONValue | undefined): Record<string, JSONValue> {
	if (config === undefined) return {};
	if (typeof config !== 'object' || config === null || Array.isArray(config)) {
		throw new Error('plugin config must be a JSON object');
	}
	return config;
}

/** Read an optional string setting and reject values outside the declared choices. */
export function stringOption(
	config: Record<string, JSONValue>,
	name: string,
	allowed: readonly string[],
): string | undefined {
	const value = config[name];
	if (value === undefined) return undefined;
	if (typeof value !== 'string' || !allowed.includes(value)) {
		throw new Error(`plugin config ${name} must be one of ${allowed.join(', ')}`);
	}
	return value;
}

/** Read an optional boolean setting; malformed config gets a named error. */
export function booleanOption(config: Record<string, JSONValue>, name: string): boolean | undefined {
	const value = config[name];
	if (value === undefined) return undefined;
	if (typeof value !== 'boolean') throw new Error(`plugin config ${name} must be boolean`);
	return value;
}
