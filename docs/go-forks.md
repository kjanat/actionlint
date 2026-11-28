# Go dependency forks

`mise run generate:go-forks` sets each version label to the version that Go selects. The `go.bytecodealliance.org/cm` label must match in the root module, the Go plugin SDK and the `insecure-download` example.

```mermaid
flowchart LR
    %% renovate: depName=github.com/kjanat/go-yaml/v4
    A[actionlint] --> Y["kjanat/go-yaml/v4<br/>v4.0.0-rc.7"]
    %% renovate: depName=github.com/kjanat/wacogo
    A --> W["kjanat/wacogo<br/>v0.3.9"]
    %% renovate: depName=github.com/kjanat/wazero
    A --> Z["kjanat/wazero<br/>v1.12.2"]
    %% renovate: depName=github.com/kjanat/go-modules
    A --> G["kjanat/go-modules<br/>v0.11.1"]
    W --> G
    W --> Z
    G --> Z
    %% renovate: depName=github.com/kjanat/go-modules/cm
    E[Go plugin SDK] --> CM["kjanat/go-modules/cm<br/>v0.5.0"]
    %% renovate: depName=go.bytecodealliance.org/cm
    G --> U["go.bytecodealliance.org/cm<br/>v0.3.0 · upstream"]
    CM --> U
    %% renovate: depName=github.com/invopop/jsonschema
    A --> J["invopop/jsonschema<br/>v0.14.0"]
    %% renovate: depName=github.com/pb33f/ordered-map/v2
    J --> O["pb33f/ordered-map/v2<br/>v2.3.2"]
    %% renovate: depName=github.com/pb33f/go-yaml
    O --> P["pb33f/go-yaml<br/>v0.2.0"]
    A -. indirect pin .-> P
```

| Fork                                                                        | Upstream                                                                                      | Role                                        |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | ------------------------------------------- |
| [kjanat/go-yaml/v4](https://github.com/kjanat/go-yaml)                      | [yaml/go-yaml](https://github.com/yaml/go-yaml)                                               | YAML decoding                               |
| [kjanat/wacogo](https://github.com/kjanat/wacogo)                           | [partite-ai/wacogo](https://github.com/partite-ai/wacogo)                                     | Component execution                         |
| [kjanat/go-modules](https://github.com/kjanat/go-modules)                   | [bytecodealliance/go-modules](https://github.com/bytecodealliance/go-modules)                 | WIT binding generation                      |
| [kjanat/go-modules/cm](https://github.com/kjanat/go-modules/tree/master/cm) | [bytecodealliance/go-modules/cm](https://github.com/bytecodealliance/go-modules/tree/main/cm) | Component Model types for the Go plugin SDK |
| [kjanat/wazero](https://github.com/kjanat/wazero)                           | [wazero/wazero](https://github.com/wazero/wazero)                                             | Core Wasm execution                         |
