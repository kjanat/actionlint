# tools.ruff.config -> {config, vars}.
#
# The input is the engine configuration: native Ruff options, with the flat
# target-version, select and ignore keys already folded into target-version,
# lint.select and lint.ignore by configuration decoding. An empty selection is
# invalid on the command line, so it selects and ignores ALL, which disables
# lint codes without hiding syntax errors.
#
# target-version, lint.select and lint.ignore become the target_version,
# select and ignore variables, which the manifest passes as --target-version,
# --select and --ignore after the user's arguments. A configured
# target-version replaces the target inferred from the workflow's Python
# shell, and repository configuration decides the rule selection over
# --select or --ignore in tools.ruff.backend.process.args.
(. // {})
| if .lint.select == [] then .lint.select = ["ALL"] | .lint.ignore = ["ALL"] else . end
| . as $config
| {
    config: (del(.["target-version"], .lint.select, .lint.ignore) | if .lint == {} then del(.lint) else . end),
    vars: (
      (if $config["target-version"] != null then {target_version: $config["target-version"]} else {} end)
      + (if $config.lint.select != null then {select: ($config.lint.select | join(","))} else {} end)
      + (if ($config.lint.ignore // []) != [] then {ignore: ($config.lint.ignore | join(","))} else {} end)
    )
  }
