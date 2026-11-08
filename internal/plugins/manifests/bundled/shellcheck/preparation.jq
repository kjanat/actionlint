def header_lines:
  split("\n")
  | reduce .[] as $line ({lines: [], ended: false};
      if .ended then .
      elif ($line | test("^[\\t \\r]*(#|$)")) then .lines += [$line]
      else .ended = true end)
  | .lines[];
def selected_shell:
  [header_lines | capture("^[\\t ]*#[\\t ]*shellcheck[\\t ]+(?:[a-z-]+=(?:\"[^\"\\r\\n]*\"|'[^'\\r\\n]*'|[^'\"#\\t \\r\\n][^#\\t \\r\\n]*)[\\t ]+)*?shell=(?<value>.*)$") | .value][0] as $value
  | if $value == null then {selected: false}
    else ($value | sub("\r$"; "")) as $value
    | {selected: true, language:
        (if ($value | startswith("\"") or startswith("'"))
         then ($value[1:] | split($value[0:1])) as $parts
              | if ($parts | length) > 1 then $parts[0] else "" end
         else ($value | split(" "))[0] end)}
    end;
. as $context
| (.source | selected_shell) as $selection
| (.settings.language // .language) as $configured
| (if $selection.selected then $selection.language else $configured end) as $language
| (if .settings.read_access.enabled == null then true else .settings.read_access.enabled end) as $enabled
| (.filesystem and (.engine | not) and $enabled) as $sources
| (["SC1091", "SC2194", "SC2050", "SC2157", "SC2043"]
   + (if .environment_known then [] else ["SC2153", "SC2154"] end)) as $excluded
| (if $language == .language then .startup else "" end) as $startup
| ([$startup, .environment_startup] | map(select(. != "")) | join("; ")) as $startup
| if (.input | not) and .language == "" and ($selection.selected | not)
  then {skip: "cannot infer a supported shell dialect; a leading # shellcheck shell=bash (or another supported dialect) selects it explicitly"}
  else {
    source_prefix: ((.settings.source_prefix // "")
      + (if ($sources or .input) then "" else "# shellcheck external-sources=false\n" end)),
    startup: $startup,
    excluded: $excluded,
    config: (if .engine then {shell: $language, exclude: $excluded, "external-sources": false} else null end),
    variables: (if .engine then null else
      {exclude: ($excluded | join("\n"))}
      + (if ($selection.selected | not) and $language != "" then {dialect: $language} else {} end)
      + (if .config_file != "" then {config_file: .config_file} else {} end)
      + (if $sources then {external_sources: "true"} else {} end)
    end)
  } end
