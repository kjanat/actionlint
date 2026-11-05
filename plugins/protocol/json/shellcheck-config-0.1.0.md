# ShellCheck configuration profile 0.1.0

Profile identifier: `kjanat:actionlint/shellcheck-config@0.1.0`.

The host submits configuration matching the adjacent JSON schema to
`tool-integration.negotiate`. A plugin may decline valid profile configuration
that its engine cannot implement. Schema validity alone never implies support.
The host must not infer support from the package name or engine identity.

The second negotiation argument is an explicit plugin-owned options object.
Its fields override corresponding profile values. The guest validates the
effective options and derives resource requests after applying those overrides.
The host validates explicit options against the analyzer's advertised schema
before negotiation; malformed caller options must not trigger native fallback.

`shell`, `disable`, `enable` and `extended-analysis` follow ShellCheck's directive
syntax. `rc-file` selects a native configuration file; plugins without rc-file
support decline it. Code ranges and `all` are valid profile inputs, but engines
supporting only individual diagnostic codes decline those selections.

`source-path` entries are host-expanded paths, with `SCRIPTDIR` and
`SCRIPTDIR/…` preserved as ShellCheck search directives. Relative paths resolve
against the run's effective working directory. An explicit `external-sources`
value takes precedence; when omitted, managed source access is enabled only if
`source-path` is nonempty. A successful negotiation requests workspace and
directory resources explicitly. These requests are not grants: host policy
still controls access and must reject unsupported requests.

Managed filesystem grants are confined to the workspace root selected by the
host. ShellCheck configuration enables source access; an explicit false denies
every resource request. Repository source paths and plugin options cannot expand
the workspace boundary. Configured paths and requested subdirectories must stay
within it after symlink resolution. Child grants open through the workspace's
existing directory handle, preserving containment against concurrent changes.
Source paths that resolve outside the workspace, including configured symlink
escapes, fail analysis. Missing paths receive no filesystem grant.

Negotiated options belong to the plugin. Host filesystem metadata never enters
those options. The `source-files` WIT import supplies typed host/guest mount
translations for the granted invocation; WASI enforces actual read access.
The guest resolves option search paths through the granted translations.
Diagnostic paths inside grants are translated back
to their original source by the host.
