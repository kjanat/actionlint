import type { Descriptor, Input, Outcome } from './.generated/plugin/interfaces/exports/kjanat-actionlint-analyzer.js';
import type * as Plugin from './.generated/plugin/plugin.js';

declare const plugin: typeof Plugin;
declare const input: Input;
const descriptor: Descriptor = plugin.analyzer.describe();
const outcome: Outcome = plugin.analyzer.analyze(input);
const selected: string[] | undefined = input.selectedScripts;
const error: string | undefined = outcome.error;
void [descriptor, selected, error];
// @ts-expect-error analyze requires the complete canonical input record.
plugin.analyzer.analyze({ path: 'workflow.yml', content: '' });
// @ts-expect-error WIT option<string> is not a numeric error code.
const badError: number = outcome.error;
void badError;
