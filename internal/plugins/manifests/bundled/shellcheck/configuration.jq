def quoted_path:
  if (length == 0 or test("[\\r\\n\\x00]")) then error("source-path: expected a nonempty, single-line path")
  elif (contains("\"") | not) then "\"" + . + "\""
  elif (contains("'") | not) then "'" + . + "'"
  elif (test("[ \\t]|^[\"']") | not) then .
  else error("source-path: ShellCheck cannot represent a path containing whitespace and both quote types") end;
. as $request
| .config
| with_entries(select(.value != null))
| if .enable == "all" or .enable == true then .enable = ["all"] else . end
| . as $options
| {
    options: ($options + (if $request.config_file != "" then {"rc-file": $request.config_file} else {} end)),
    language: .shell,
    read_access: {enabled: .["external-sources"], directories: (.["source-path"] // [] | map(select(. != "SCRIPTDIR" and (startswith("SCRIPTDIR/") | not))))},
    source_prefix: ([
      (if (.disable // [] | length) > 0 then "# shellcheck disable=" + (.disable | join(",")) + "\n" else empty end),
      (if (.enable // [] | length) > 0 then "# shellcheck enable=" + (.enable | join(",")) + "\n" else empty end),
      (if has("extended-analysis") then "# shellcheck extended-analysis=" + (.["extended-analysis"] | tostring) + "\n" else empty end),
      (.["source-path"] // [] | .[] | "# shellcheck source-path=" + quoted_path + "\n")
    ] | join(""))
  }
