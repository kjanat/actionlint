# Runny semantic backend

`Runny` compiles Runny's expression WASM once and parses a batch in a fresh
WASI instance. It uses the original runner's `next` parser, without evaluating
expressions or supplying execution context. Results identify the runner revision.

The host grants no filesystem directories, environment variables or network
access. Memory, fuel, input and output have limits. Traps, incomplete responses,
wrong request IDs and inconsistent parser results return errors.

The backend implements the engine's analyzer interface over decoded expression
facts. Runner syntax errors produce diagnostics bound to the captured source;
runtime and protocol failures fail analysis. CLI/LSP selection and plugin fact
queries are not connected yet. Expression evaluation and partially known context
are not implemented.

## Test artifact

From the workspace root:

```sh
gh release download v0.2.0 --repo kjanat/runny-mc-fuzzface \
  --pattern runny-v0.2.0-expression-full.wasm --pattern SHA256SUMS \
  --dir target/providers/runny-v0.2.0
```

Verify the WASM SHA-256 against its entry in `SHA256SUMS`, then run:

```sh
cargo test -p actionlint-runny --test runny
```

`RUNNY_WASM` can select another copy of the same artifact. Missing artifacts fail
the test. The test checks a mixed valid/invalid batch, unknown context references,
Unicode and a second invocation of the compiled module.
