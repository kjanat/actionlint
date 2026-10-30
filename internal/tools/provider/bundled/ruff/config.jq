# tools.ruff.config -> {config, vars}.
#
# The input is the engine configuration: native Ruff options, with the flat
# target-version, select and ignore keys already folded into target-version,
# lint.select and lint.ignore by configuration decoding. An empty selection is
# invalid on the command line, so it selects and ignores ALL, which disables
# lint codes without hiding syntax errors. A configured target-version
# replaces the target inferred from the workflow's Python shell, so the
# target_version variable is dropped.
(. // {})
| if .lint.select == [] then .lint.select = ["ALL"] | .lint.ignore = ["ALL"] else . end
| {config: ., vars: (if .["target-version"] != null then {target_version: null} else {} end)}
