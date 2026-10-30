# tools.ruff.config -> {config, vars}.
#
# The input is the engine configuration: native Ruff options with the flat
# select and ignore keys folded in. They become lint.select and lint.ignore.
# An empty selection is invalid on the command line, so it selects and
# ignores ALL, which disables lint codes without hiding syntax errors. A
# configured target-version replaces the target inferred from the workflow's
# Python shell, so the target_version variable is dropped.
def fold($key): if .[$key] != null then .lint[$key] = .[$key] else . end | del(.[$key]);
(. // {})
| fold("select")
| fold("ignore")
| if .lint.select == [] then .lint.select = ["ALL"] | .lint.ignore = ["ALL"] else . end
| {config: ., vars: (if .["target-version"] != null then {target_version: null} else {} end)}
