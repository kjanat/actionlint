/** Rejected syntax stays in test data and runs through isolated probe workflows. */
export type Check = { name: string; expression: string };
export type Group = 'literals' | 'from-json' | 'coercion' | 'typed-inputs' | 'decisions';
type Common = { id: string; group: Group };
export type ProbeCase =
	& Common
	& (
		| { mode: 'dual'; check: Check }
		| {
			mode: 'runner';
			checks: Check[];
			input: { type: 'string' | 'number'; value: string | number } | null;
		}
	);

export function quoteExpression(value: string): string {
	return `'${value.replaceAll("'", "''")}'`;
}

const literals = [
	'+1',
	'-0',
	'-1',
	'-Infinity',
	'.5',
	'.5e1',
	'0',
	'0.1 + 0.2',
	'00',
	'0123',
	'0B101',
	'0O17',
	'0X1F',
	'0b101',
	'0o17',
	'0o8',
	'0x01',
	'0x0123',
	'0x0e1',
	'0x1F',
	'0xG',
	'1 + 1',
	'1.',
	'1.0e+',
	'1.0e+99',
	'1.5',
	'1E+5',
	'1_000',
	'1e',
	'1e+',
	'1e+01',
	'1e+5 == 100000',
	'1e+5',
	'1e-0',
	'1e-5',
	'1e01',
	'1e0123',
	'1e309',
	'1e5',
	'1e5e5',
	'2147483648',
	'9007199254740993',
	'Infinity',
	'NaN',
];

const payloads = [
	['block-comment', '/* comment */31'],
	['comment-after-value', '{"approved":true/*x*/,"n":1}'],
	['comment-before-denial', '/* attacker comment */{"approved":false,"role":"user","n":1}'],
	['comment-before-value', '{"approved":/*x*/true,"n":1}'],
	['decimal', '31'],
	['decision-baseline', '{"approved":true,"role":"admin","n":1}'],
	['duplicate-number', '{"n":1,"n":2}'],
	['duplicate-role', '{"role":"user","role":"admin"}'],
	['hex', '0x1F'],
	['infinity', 'Infinity'],
	['infinity-approved', '{"n":Infinity,"approved":true}'],
	['leading-comment-object', '/* comment */{"n":1}'],
	['leading-dot', '.5'],
	['leading-zero', '017'],
	['line-comment', '// comment\n31'],
	['nan', 'NaN'],
	['nan-approved', '{"n":NaN,"approved":true}'],
	['negative-infinity', '-Infinity'],
	['object-hex', '{"n":0x1F}'],
	['object-leading-zero', '{"n":017}'],
	['object-nan', '{"n":NaN}'],
	['octal-prefix', '0o17'],
	['overflow-approved', '{"n":1e309,"approved":true}'],
	['plus', '+1'],
	['single-quotes', "{'role':'admin'}"],
	['trailing-comma-array', '[1,]'],
	['trailing-comma-object', '{"role":"admin",}'],
	['trailing-comment', '{"n":1} // comment'],
	['trailing-dot', '1.'],
	['trailing-text-number', '31 trailing'],
	['trailing-text-object', '{"n":1} garbage'],
	['two-objects', '{"n":1}{"n":2}'],
	['undefined', 'undefined'],
	['unquoted-property', "{role:'admin'}"],
];

const payloadChecks: Check[] = [
	{ name: 'approved-false', expression: 'fromJSON(inputs.value).approved == false' },
	{ name: 'approved-not-false', expression: 'fromJSON(inputs.value).approved != false' },
	{ name: 'approved-true', expression: 'fromJSON(inputs.value).approved == true' },
	{ name: 'n', expression: 'fromJSON(inputs.value).n' },
	{ name: 'n-ge-0', expression: 'fromJSON(inputs.value).n >= 0' },
	{ name: 'n-gt-100', expression: 'fromJSON(inputs.value).n > 100' },
	{ name: 'n-le-100', expression: 'fromJSON(inputs.value).n <= 100' },
	{ name: 'n-not-gt-100', expression: '!(fromJSON(inputs.value).n > 100)' },
	{ name: 'n-not-lt-0', expression: '!(fromJSON(inputs.value).n < 0)' },
	{ name: 'n-positive', expression: 'fromJSON(inputs.value).n > 0' },
	{ name: 'parsed', expression: 'fromJSON(inputs.value)' },
	{ name: 'parsed-null', expression: 'fromJSON(inputs.value) == null' },
	{ name: 'role-admin', expression: "fromJSON(inputs.value).role == 'admin'" },
	{ name: 'role-not-admin', expression: "fromJSON(inputs.value).role != 'admin'" },
];

const cases: ProbeCase[] = literals.map((expression, index) => ({
	id: `literal-${String(index + 1).padStart(2, '0')}`,
	group: 'literals',
	mode: 'dual',
	check: { name: 'value', expression },
}));

for (const [id, value] of payloads) {
	if (id === undefined || value === undefined) throw new Error('Incomplete payload case');
	cases.push({
		id: `json-${id}`,
		group: 'from-json',
		mode: 'runner',
		input: { type: 'string', value },
		checks: payloadChecks,
	});
}

// Compare the same input string through loose coercion and fromJSON. Keep the
// coercion checks in one workflow: all syntax is valid and none invokes a parser.
const coercions: Check[] = [];
for (const raw of ['0x1F', '017', '0o17', 'Infinity', 'NaN', '31', '', ' ', '+1', '.5', '1.', '0123', '1e309']) {
	for (
		const [label, suffix] of [
			['eq-123', '== 123'],
			['eq-15', '== 15'],
			['eq-17', '== 17'],
			['eq-31', '== 31'],
			['eq-half', '== 0.5'],
			['eq-infinity', '== Infinity'],
			['eq-nan', '== NaN'],
			['eq-one', '== 1'],
			['eq-zero', '== 0'],
			['gt-30', '> 30'],
			['lt-32', '< 32'],
			['ne-nan', '!= NaN'],
		]
	) {
		if (label === undefined || suffix === undefined) throw new Error('Incomplete coercion check');
		coercions.push({ name: `${JSON.stringify(raw)} ${label}`, expression: `${quoteExpression(raw)} ${suffix}` });
	}
}
cases.push({
	id: 'coercion-strings',
	group: 'coercion',
	mode: 'runner',
	input: null,
	checks: coercions,
});

for (const value of ['31', '0x1F', 'Infinity', 'NaN', 31]) {
	cases.push({
		id: `input-${typeof value}-${value}`,
		group: 'typed-inputs',
		mode: 'runner',
		input: { type: 'number', value },
		checks: [
			{ name: 'event-eq-31', expression: 'github.event.inputs.value == 31' },
			{ name: 'event-value', expression: 'github.event.inputs.value' },
			{ name: 'inputs-eq-31', expression: 'inputs.value == 31' },
			{ name: 'inputs-value', expression: 'inputs.value' },
		],
	});
}

cases.push({
	id: 'decision-pitfalls',
	group: 'decisions',
	mode: 'runner',
	input: null,
	checks: [
		{ name: 'boolean-false-falsy', expression: "false && 'runs' || 'skips'" },
		{ name: 'string-false-truthy', expression: "'false' && 'runs' || 'skips'" },
		{ name: 'case-insensitive-role', expression: "'ADMIN' == 'admin'" },
		{ name: 'falsy-ternary-arm', expression: "true && 0 || 'fallback'" },
		{ name: 'leading-zero-literal', expression: '017' },
		{ name: 'missing-equals-empty', expression: "fromJSON('{}').role == ''" },
		{ name: 'missing-equals-false', expression: "fromJSON('{}').approved == false" },
		{ name: 'missing-equals-zero', expression: "fromJSON('{}').n == 0" },
		{ name: 'nan-rejects-upper-bound', expression: 'NaN > 100' },
		{ name: 'negated-upper-bound-allows-nan', expression: '!(NaN > 100)' },
		{ name: 'positive-range-rejects-nan', expression: 'NaN >= 0 && NaN <= 100' },
		{ name: 'rounded-integer', expression: '9007199254740993 == 9007199254740992' },
	],
});

export const corpus: readonly ProbeCase[] = cases;
export function checksFor(probe: ProbeCase): Check[] {
	return probe.mode === 'dual' ? [probe.check] : probe.checks;
}
