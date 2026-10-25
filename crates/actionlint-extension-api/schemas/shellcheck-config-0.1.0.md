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

`shell`, `disable`, `enable` and `extended-analysis` retain ShellCheck directive
semantics. `rc-file` selects a native configuration file; plugins without rc-file
support decline it. Code ranges and `all` are valid profile inputs, but engines
supporting only individual diagnostic codes decline those selections.

`source-path` entries are host-expanded paths, with `SCRIPTDIR` and
`SCRIPTDIR/…` preserved as ShellCheck search directives. Relative paths resolve
against the run's effective working directory. An explicit `external-sources`
value takes precedence; when omitted, managed source access is enabled only if
`source-path` is nonempty. A successful negotiation requests workspace and
directory resources explicitly. These requests are not grants: host policy
still controls access and must reject unsupported requests.

Managed filesystem authority comes only from the host's ShellCheck configuration.
Access must be enabled there; an explicit false denies every resource request.
The workspace and configured source-path roots bound the grants. Requested
subdirectories must remain within those roots after symlink resolution, and
opening a child grant must preserve that containment against concurrent changes.
Plugin options may narrow access but cannot authorize additional host paths or
turn disabled host access on. Configured symlink roots authorize their resolved
targets; incidental symlinks escaping an authorized root do not.

Negotiated options belong to the plugin. Host filesystem metadata never enters
those options. The `source-files` WIT import supplies typed host/guest mount
translations for the granted invocation; WASI enforces actual read access.
Search paths remain unchanged in options and the guest resolves them through
the granted translations. Diagnostic paths inside grants are translated back
to their original source by the host.
