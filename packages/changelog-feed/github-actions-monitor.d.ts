import type { FeedItem } from '#feed';
import type { AsyncFunctionArguments } from '@actions/github-script';

export type RunArguments = Pick<AsyncFunctionArguments, 'github' | 'context' | 'core'>;

export interface ChangelogEntry {
	guid: string;
	link: string;
	title: string;
	published: string;
	summary: string;
	type: string;
	labels: string[];
}

export const LABEL: string;
export const LABEL_COLOR: string;
export const LABEL_DESCRIPTION: string;
export const FEED_LABEL: string;

export function marker(guid: string): string;

export function toEntry(item: FeedItem): ChangelogEntry;

export function reportedGuids(issues: Iterable<{ body?: string | null }>): Set<string>;

export function selectEntries(
	entries: ChangelogEntry[],
	options: { reported: Set<string>; since: Date; limit: number },
): ChangelogEntry[];

export function issueTitle(entry: ChangelogEntry): string;

export function issueBody(entry: ChangelogEntry): string;

export default function run(args: RunArguments): Promise<void>;
