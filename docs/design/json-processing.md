# JSON processing and Go version compatibility

The module requires Go 1.27 and prefers the Go 1.27.2 toolchain. Core WebAssembly
engine metadata and response decoding use `encoding/json/v2`: duplicate object
keys and invalid UTF-8 are rejected, and protocol fields match case-sensitively.
Unknown fields remain accepted so engines can extend their metadata and responses.

Request encoding, public JSON output, and expression evaluation retain the existing
`encoding/json` behavior. The [Go 1.27 release notes](https://go.dev/doc/go1.27#jsonv2)
document that this API now uses the v2 implementation with compatibility options.

## Migration candidates

| Boundary                                                       | Assessment                                                                                                                                                                                                     |
| -------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/plugins/core` metadata and response decoding         | Migrated to v2. Rejecting duplicate object keys and invalid UTF-8 is appropriate for an engine protocol. Keep unknown fields extensible, and test missing fields and operation-version negotiation separately. |
| `internal/ruff/diagnostic.go` and ShellCheck response decoding | Candidate after checking the real checker outputs and validating case-sensitive field matching. Protocol failures must still fail analysis.                                                                    |
| `internal/actioncatalog` and conformance source manifests      | Controlled inputs are straightforward candidates. Re-run the generators and fixture readers to establish compatibility.                                                                                        |
| `internal/render` JSON, JSONL, SARIF, and template output      | Preserve the published representation deliberately. Review nil collections, `omitempty`, escaping, deterministic map order, custom marshalers, and final newlines before changing APIs.                        |
| `internal/report` document and action variants                 | Migrate together with their codecs and consumers. These contain custom marshalers and raw JSON discriminated unions. An import replacement alone does not cover the contract.                                  |
| `internal/ruffconfig` and native Ruff configuration            | Preserve exact integer handling currently supplied by `Decoder.UseNumber`; do not round values through `float64`.                                                                                              |
| `internal/expression` JSON evaluation                          | Treat this as language behavior. Duplicate keys, number handling, escaping, and invalid Unicode must follow established expression behavior, independently of a JSON library's defaults.                       |

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

The migration uses one implementation across supported toolchains, without build
tags or experimental flags. The minimum version in `go.mod` enforces availability
of the stable v2 API for source builds and library consumers.
