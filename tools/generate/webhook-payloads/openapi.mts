// Preserve the OpenAPI graph, including requiredness, nulls and alternatives.
export type Property = { type: number; optional: boolean; description?: string };
export type Shape = {
	kind: string;
	properties?: Record<string, Property>;
	element?: number;
	mapped?: number;
	alternatives?: number[];
	literal?: string;
	required?: string[];
	additionalProperties?: 'allowed' | 'forbidden' | 'schema';
};

function isObject(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function object(value: unknown): Record<string, unknown> {
	if (!isObject(value)) {
		throw new Error('Expected an OpenAPI object');
	}
	return value;
}

export function convertOpenAPI(input: unknown) {
	const document = object(input);
	const nodes: Shape[] = [];
	const seen = new Map<unknown, number>();
	function resolve(reference: string): unknown {
		if (!reference.startsWith('#/')) throw new Error(`External reference: ${reference}`);
		let value: unknown = document;
		for (const part of reference.slice(2).split('/')) {
			value = object(value)[part.replaceAll('~1', '/').replaceAll('~0', '~')];
		}
		if (value === undefined) throw new Error(`Unresolved reference: ${reference}`);
		return value;
	}
	function convert(value: unknown): number {
		const cached = seen.get(value);
		if (cached !== undefined) return cached;
		const id = nodes.length;
		const node: Shape = { kind: 'any' };
		nodes.push(node);
		seen.set(value, id);
		if (value === true) return id;
		if (value === false) {
			node.kind = 'absent';
			return id;
		}
		const schema = object(value);
		if (Array.isArray(schema.required)) {
			node.required = schema.required.map(name => {
				if (typeof name !== 'string') throw new Error('Required property name must be a string');
				return name;
			});
		}
		if (schema.nullable === true) {
			node.kind = 'union';
			node.alternatives = [convert({ ...schema, nullable: false }), convert({ type: 'null' })];
			return id;
		}
		if (typeof schema.$ref === 'string') {
			node.kind = 'intersection';
			node.alternatives = [convert(resolve(schema.$ref))];
			const siblings = { ...schema };
			delete siblings.$ref;
			if (Object.keys(siblings).some(key => !['description', 'title', 'summary'].includes(key))) {
				node.alternatives.push(convert(siblings));
			}
			return id;
		}
		for (const keyword of ['allOf', 'oneOf', 'anyOf']) {
			const alternatives = schema[keyword];
			if (!Array.isArray(alternatives)) continue;
			node.kind = keyword === 'allOf' ? 'intersection' : 'union';
			node.alternatives = alternatives.map(convert);
			const siblings = { ...schema };
			delete siblings[keyword];
			if (
				['type', 'properties', '$ref', 'allOf', 'oneOf', 'anyOf', 'additionalProperties'].some(key =>
					siblings[key] !== undefined
				)
			) {
				const alternativesId = nodes.length;
				nodes.push({ ...node });
				node.kind = 'intersection';
				node.alternatives = [alternativesId, convert(siblings)];
			}
			return id;
		}
		if (Array.isArray(schema.type)) {
			node.kind = 'union';
			node.alternatives = schema.type.map(type => convert({ ...schema, type }));
			return id;
		}
		switch (schema.type) {
			case 'null':
			case 'boolean':
			case 'string':
				node.kind = schema.type;
				if (Array.isArray(schema.enum) && schema.enum.length === 1 && typeof schema.enum[0] === 'string') {
					node.literal = schema.enum[0];
				}
				break;
			case 'integer':
			case 'number':
				node.kind = 'number';
				break;
			case 'array':
				node.kind = 'array';
				node.element = convert(schema.items ?? true);
				break;
			case 'object':
			case undefined: {
				if (schema.type === undefined && schema.properties === undefined && schema.additionalProperties === undefined) {
					break;
				}
				node.kind = 'object';
				node.properties = {};
				if (schema.additionalProperties !== undefined) {
					node.additionalProperties = schema.additionalProperties === false
						? 'forbidden'
						: schema.additionalProperties === true
						? 'allowed'
						: 'schema';
				}
				const required = new Set(Array.isArray(schema.required) ? schema.required : []);
				for (
					const [name, value] of Object.entries(object(schema.properties ?? {})).sort(([a], [b]) =>
						a.localeCompare(b, 'en')
					)
				) {
					const description = typeof value === 'boolean' ? undefined : object(value).description;
					node.properties[name] = {
						type: convert(value),
						optional: !required.has(name),
						...(typeof description === 'string' ? { description } : {}),
					};
				}
				if (schema.additionalProperties !== undefined && schema.additionalProperties !== false) {
					node.mapped = convert(schema.additionalProperties);
				} else if (schema.additionalProperties !== false && Object.keys(node.properties).length === 0) {
					node.mapped = convert(true);
				}
				break;
			}
			default:
				throw new Error(`Unknown schema type: ${schema.type}`);
		}
		return id;
	}
	const events: Record<string, Record<string, number>> = {};
	for (const [, value] of Object.entries(object(document.webhooks)).sort(([a], [b]) => a.localeCompare(b, 'en'))) {
		const operation = object(object(value).post);
		if (typeof operation.operationId !== 'string') throw new Error('Missing webhook operationId');
		const [event = '', action = ''] = operation.operationId.split('/');
		const name = event.replaceAll('-', '_');
		const content = object(object(operation.requestBody).content);
		const schema = object(content['application/json']).schema;
		events[name] ??= {};
		events[name][action.replaceAll('-', '_')] = convert(schema);
	}
	return { events, nodes };
}
