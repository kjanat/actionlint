# Ruff `--output-format json` output -> script diagnostics.
#
# Ruff reports the absolute path of --stdin-filename; the snippet's virtual
# name replaces it. Diagnostics without a code, such as preview syntax errors,
# are named by their rule name. Null members are dropped.
def position: if . == null then null else {row, column} end;
[
  .[]
  | {
      filename: (if .filename == $vars.filename_abs then $vars.filename else .filename end),
      code: (.code // .name),
      name,
      message,
      severity,
      location: (.location | position),
      end_location: (.end_location | position),
      url
    }
  | with_entries(select(.value != null))
]
