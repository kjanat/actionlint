# TypeScript Component Model contract checks

Run `npm ci --ignore-scripts` and `npm test` in this directory, or run
`run test:component-types` after installing its dependencies.

The check generates declarations directly from the canonical analyzer world and
both runner-provider versions using Jco 1.42.0. Strict consumer compilation checks
the analyzer input and error representation. A small resource fixture checks the
explicit nominal-resource and named-error options, including rejection of host
mocks where the generated guest resource identity is required. Generated files
are temporary and are not another copy of the protocol.

Use the same declaration options when transpiling a component. This check does
not componentize JavaScript, validate guest binaries or replace the Go host's
admission checks. The published TypeScript SDK continues to implement process
plugins. These fixtures exercise a possible component SDK independently.
