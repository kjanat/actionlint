import assert from 'node:assert/strict';
import { test } from 'node:test';

import { convertOpenAPI, type Shape } from './openapi.mts';

function catalogue(schema: unknown, components: unknown = {}) {
	return convertOpenAPI({
		components: { schemas: components },
		webhooks: {
			sample: { post: { operationId: 'sample/created', requestBody: { content: { 'application/json': { schema } } } } },
		},
	});
}

function shape(nodes: Shape[], id: number | undefined): Shape {
	assert.notEqual(id, undefined);
	const result = id === undefined ? undefined : nodes[id];
	assert.ok(result);
	return result;
}

test('required, optional and nullable properties remain distinct', () => {
	const { nodes, events } = catalogue({
		type: 'object',
		required: ['id', 'body'],
		properties: {
			id: { type: 'integer' },
			body: { type: ['string', 'null'] },
			optional: { type: 'boolean', description: 'An optional flag' },
		},
	});
	const root = shape(nodes, events.sample?.created);
	assert.equal(root.properties?.id?.optional, false);
	assert.equal(root.properties?.body?.optional, false);
	assert.equal(root.properties?.optional?.optional, true);
	assert.equal(root.properties?.optional?.description, 'An optional flag');
	assert.equal(shape(nodes, root.properties?.id?.type).kind, 'number');
	const body = shape(nodes, root.properties?.body?.type);
	assert.equal(body.kind, 'union');
	assert.deepEqual(body.alternatives?.map(id => shape(nodes, id).kind), ['string', 'null']);
});

test('references, intersections, alternatives and recursion survive conversion', () => {
	const { nodes, events } = catalogue({ $ref: '#/components/schemas/root' }, {
		root: {
			allOf: [
				{ type: 'object', properties: { child: { $ref: '#/components/schemas/root' } } },
				{ oneOf: [{ type: 'string' }, { type: 'array', items: { type: 'boolean' } }] },
			],
		},
	});
	const reference = shape(nodes, events.sample?.created);
	const rootId = reference.alternatives?.[0];
	const root = shape(nodes, rootId);
	assert.equal(root.kind, 'intersection');
	const object = shape(nodes, root.alternatives?.[0]);
	const child = shape(nodes, object.properties?.child?.type);
	assert.deepEqual(child.alternatives, [rootId]);
	assert.equal(shape(nodes, root.alternatives?.[1]).kind, 'union');
});

test('bare and explicitly dynamic objects stay open, closed objects stay closed', () => {
	for (const schema of [{ type: 'object' }, { type: 'object', additionalProperties: true }]) {
		const { nodes, events } = catalogue(schema);
		assert.equal(shape(nodes, shape(nodes, events.sample?.created).mapped).kind, 'any');
	}
	const { nodes, events } = catalogue({ type: 'object', additionalProperties: false });
	assert.equal(shape(nodes, events.sample?.created).mapped, undefined);
});

test('bad and remote references fail instead of silently dropping schema', () => {
	assert.throws(() => catalogue({ $ref: '#/components/schemas/missing' }), /Unresolved reference/);
	assert.throws(() => catalogue({ $ref: 'https://example.com/schema.json' }), /External reference/);
});

test('composition retains required constraints and legacy nullable wrappers', () => {
	const { nodes, events } = catalogue({
		nullable: true,
		required: ['id'],
		allOf: [{ type: 'object', properties: { id: { type: 'integer' } } }],
	});
	const root = shape(nodes, events.sample?.created);
	assert.equal(root.kind, 'union');
	assert.deepEqual(root.required, ['id']);
	const intersection = shape(nodes, root.alternatives?.[0]);
	assert.equal(intersection.kind, 'intersection');
	assert.deepEqual(intersection.required, ['id']);
	assert.equal(shape(nodes, root.alternatives?.[1]).kind, 'null');
});

test('sibling compositions are intersected without losing required-only constraints', () => {
	const { nodes, events } = catalogue({
		required: ['id'],
		allOf: [{ type: 'object', properties: { id: { type: 'integer' } } }],
		oneOf: [{ type: 'object', properties: { first: { type: 'string' } } }, { required: ['second'] }],
	});
	const root = shape(nodes, events.sample?.created);
	assert.equal(root.kind, 'intersection');
	assert.deepEqual(root.required, ['id']);
	const conjunction = shape(nodes, root.alternatives?.[0]);
	assert.equal(conjunction.kind, 'intersection');
	const alternatives = shape(nodes, root.alternatives?.[1]);
	assert.equal(alternatives.kind, 'union');
	assert.deepEqual(alternatives.required, ['id']);
	assert.deepEqual(shape(nodes, alternatives.alternatives?.[1]).required, ['second']);
});

test('additionalProperties default and explicit policies are distinguishable', () => {
	for (
		const [extra, policy] of [
			[{}, undefined],
			[{ additionalProperties: false }, 'forbidden'],
			[{ additionalProperties: true }, 'allowed'],
			[{ additionalProperties: { type: 'number' } }, 'schema'],
		]
	) {
		assert.ok(typeof extra === 'object' && extra !== null);
		const { nodes, events } = catalogue({ type: 'object', properties: { name: { type: 'string' } }, ...extra });
		assert.equal(shape(nodes, events.sample?.created).additionalProperties, policy);
	}
});
