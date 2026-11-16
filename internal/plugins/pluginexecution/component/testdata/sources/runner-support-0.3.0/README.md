# Runner support fixture

This component exports runner-provider 0.3.0. It echoes the request service and
date in the candidate name and returns a dated support policy. Snapshot content
selects malformed replies for host admission tests.

Rebuild from the repository root:

```powershell
$env:CARGO_BUILD_JOBS = '1'
mise exec -- cargo build --locked -p actionlint-fixture-runner-support-0-3-0 --target wasm32-wasip2 --profile wasm
Copy-Item -LiteralPath target/wasm32-wasip2/wasm/actionlint_fixture_runner_support_0_3_0.wasm -Destination internal/plugins/pluginexecution/component/testdata/plugins/runner-support-0.3.0.wasm
```
