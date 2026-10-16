# JSON processing and Go version compatibility

The module currently supports Go 1.26 and prefers the Go 1.27.2 toolchain. Stable
`encoding/json/v2` and `encoding/json/jsontext` require Go 1.27. An unconditional
import would raise the source-build minimum; the toolchain preference alone does
not change that compatibility contract. Go 1.26 support was restored in v1.15.0.

Verified directly with `GOTOOLCHAIN=go1.26.0 go list encoding/json/v2`: the command
exits with status 1 because build constraints exclude all Go files in that package.
Using experimental flags would introduce an additional build requirement for
consumers rather than preserve ordinary Go 1.26 builds.

The [Go 1.27 release notes](https://go.dev/doc/go1.27#jsonv2) also document that
`encoding/json` now uses the v2 implementation with compatibility options. Keeping
its API on Go 1.27 therefore retains the implementation's decoding improvements.

## Migration candidates

| Boundary                                                       | Assessment                                                                                                                                                                                                                                       |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/corewasm` metadata and response decoding             | Strong first candidate after raising the minimum. Rejecting duplicate object keys and invalid UTF-8 is appropriate for an engine protocol. Keep unknown fields extensible, and test missing fields and operation-version negotiation separately. |
| `internal/ruff/diagnostic.go` and ShellCheck response decoding | Suitable after checking the real checker outputs and validating case-sensitive field matching. Protocol failures must still fail analysis.                                                                                                       |
| `internal/actioncatalog` and conformance source manifests      | Controlled inputs are straightforward candidates. Re-run the generators and fixture readers to establish compatibility.                                                                                                                          |
| `internal/render` JSON, JSONL, SARIF, and template output      | Preserve the published representation deliberately. Review nil collections, `omitempty`, escaping, deterministic map order, custom marshalers, and final newlines before changing APIs.                                                          |
| `internal/report` document and action variants                 | Migrate together with their codecs and consumers. These contain custom marshalers and raw JSON discriminated unions. An import replacement alone does not cover the contract.                                                                    |
| `internal/ruffconfig` and native Ruff configuration            | Preserve exact integer handling currently supplied by `Decoder.UseNumber`; do not round values through `float64`.                                                                                                                                |
| `internal/expression` JSON evaluation                          | Treat this as language behavior. Duplicate keys, number handling, escaping, and invalid Unicode must follow established expression behavior, independently of a JSON library's defaults.                                                         |

## Observed behavioral differences

A local probe on Go 1.27.2 encoded a struct with a nil string slice, a false
`omitempty` boolean, and a zero `omitempty` integer as:

```json
{ "values": null }
```

With default v2 options it encoded the same value as:

```json
{ "values": [], "enabled": false, "offset": 0 }
```

The same probe showed v1 accepting `{"ok":false,"ok":true}` and an invalid UTF-8
string while v2 rejected both. Stricter protocol input validation is useful, but
changing public output or expression evaluation requires a separate compatibility
decision and regression coverage.

No JSON implementation is selected by build tags. Retaining one behavior across
supported toolchains avoids accepting different engine messages depending on the
Go version used to build actionlint. The current implementation keeps the v1 API
until the Go 1.27 minimum is adopted.
