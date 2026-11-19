# Go dependency forks

The graph shows which modules depend on which forks. Solid arrows are module
requirements; dotted arrows are indirect requirements in actionlint's root module.
Renovate updates the version labels in the same pull requests as the Go modules.

```mermaid
flowchart LR
    %% renovate: depName=github.com/kjanat/go-yaml/v4
    A[actionlint] --> Y["kjanat/go-yaml/v4<br/>v4.0.0-rc.7"]
    %% renovate: depName=github.com/kjanat/wacogo
    A --> W["kjanat/wacogo<br/>v0.3.1"]
    %% renovate: depName=github.com/kjanat/wazero
    A --> Z["kjanat/wazero<br/>v1.12.2"]
    %% renovate: depName=github.com/kjanat/go-modules
    A --> G["kjanat/go-modules<br/>v0.10.0"]
    W --> G
    W --> Z
    G --> Z
    %% renovate: depName=go.bytecodealliance.org/cm
    G --> CM["go.bytecodealliance.org/cm<br/>v0.3.0 · upstream"]
    E[Go plugin SDK] --> CM
    %% renovate: depName=github.com/invopop/jsonschema
    A --> J["invopop/jsonschema<br/>v0.14.0"]
    %% renovate: depName=github.com/pb33f/ordered-map/v2
    J --> O["pb33f/ordered-map/v2<br/>v2.3.2"]
    %% renovate: depName=github.com/pb33f/go-yaml
    O --> P["pb33f/go-yaml<br/>v0.2.0"]
    A -. indirect pin .-> P
```

The `kjanat/*` labels abbreviate `github.com/kjanat/*`. Go selects one version
per module path within each consuming module's graph; a dependency's own minimum
requirement can be lower.

| Fork                                                      | Upstream                                                                      | Role                                                                                      |
| --------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [kjanat/go-yaml/v4](https://github.com/kjanat/go-yaml)    | [yaml/go-yaml](https://github.com/yaml/go-yaml)                               | Workflow, action and configuration YAML decoding.                                         |
| [kjanat/wacogo](https://github.com/kjanat/wacogo)         | [partite-ai/wacogo](https://github.com/partite-ai/wacogo)                     | Component execution and generated host bindings.                                          |
| [kjanat/go-modules](https://github.com/kjanat/go-modules) | [bytecodealliance/go-modules](https://github.com/bytecodealliance/go-modules) | WIT loading and binding generation.                                                       |
| [kjanat/wazero](https://github.com/kjanat/wazero)         | [wazero/wazero](https://github.com/wazero/wazero)                             | Core Wasm execution, directly and through wacogo and WIT tooling.                         |
| [pb33f/go-yaml](https://github.com/pb33f/go-yaml)         | [yaml/go-yaml](https://github.com/yaml/go-yaml)                               | Schema tooling through jsonschema and ordered-map; separate from actionlint's YAML codec. |

The [root module](../go.mod) uses wacogo for components and wazero directly for
core Wasm plugins. Its [plugin-contract generator](../internal/development/generators/plugin-contract)
uses go-modules to read WIT. The root module also declares the `wacogo-witgen`
and `wit-bindgen-go` generation tools. The [Go plugin SDK](../plugins/sdk/go/go.mod)
depends only on the upstream `go.bytecodealliance.org/cm` runtime.

The selected forks' own `go.mod` files define the recursive requirements shown
above.
