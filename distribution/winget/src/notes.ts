import type { Code, Html, Nodes, Table } from 'mdast';
import { remark } from 'remark';
import remarkGfm from 'remark-gfm';
import stripMarkdown from 'strip-markdown';

const text = (node: Nodes): string =>
	'value' in node ? node.value : 'children' in node ? node.children.map(text).join('') : '';

const paragraph = (value: string): Nodes => ({ type: 'paragraph', children: [{ type: 'text', value }] });

const bullet = (line: string, index: number): string => index === 0 ? `- ${line}` : line === '' ? line : `  ${line}`;

const processor = remark().use(remarkGfm).use(stripMarkdown, {
	keep: ['list', 'listItem'],
	remove: [
		['code', (node: Code) => paragraph(node.value)],
		['html', (node: Html) => paragraph(node.value.replaceAll(/<[^>]*>/g, ' ').replaceAll(/[ \t]+/g, ' ').trim())],
		['table', (node: Table) => paragraph(node.children.map(row => row.children.map(text).join(' | ')).join('\n'))],
	],
}).freeze();

function blocks(node: Nodes): string[] {
	switch (node.type) {
		case 'paragraph':
			return [text(node)];
		case 'list':
			return [node.children.map(item => blocks(item).join('\n').split('\n').map(bullet).join('\n')).join('\n')];
		default:
			return 'children' in node ? node.children.flatMap(blocks) : [];
	}
}

export function plainText(markdown: string): string {
	return blocks(processor.runSync(processor.parse(markdown))).join('\n\n');
}
