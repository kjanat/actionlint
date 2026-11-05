# Shell preparation 0.2.0

`kjanat:actionlint/prepared-scripts@0.2.0` centralizes the preparation used by
native shell analysis and component analyzers. Guests consume its returned
source, dialect, exclusions and line mapping without reimplementing preparation.
The callback accepts only run occurrence IDs whose script text was supplied in the current analyze call. Known but unselected occurrences, action inputs, calls, the entry and unknown IDs return an error. Occurrence identity preserves distinct invocation context when declarations are shared.

The runtime shell, when known, takes precedence over the declared shell. Supported
inferred dialects are bash, sh, dash and ksh. An initial `shellcheck shell=`
directive may select another supported engine dialect. Attached YAML ShellCheck
comments precede script header directives. A directive takes precedence over the
requested configuration dialect. An unsupported inferred shell without a
directive returns none. Unknown or foreign occurrence IDs return an error.

Bare explicit bash receives `set -e -o pipefail`; implicit bash and bare sh receive
`set -e`. Custom shell command options are interpreted by the host's shared native
shell parser. Only final enabled options are emitted; dynamic or quoted arguments
do not synthesize startup options. A dialect override differing from the runtime
dialect removes startup options.

Every character of a complete `${{ ... }}` substitution becomes `_`, except CR
and LF. The replacement has the same Unicode scalar width and line boundaries.
Unmatched opening delimiters are copied verbatim. A shebang stays first, followed by attached
directives, the original leading comments, synthesized startup, and script body.

Returned line entries distinguish original script lines, attached document
comments, and synthesized startup. Guests use script spans from analyzer WIT to
map original script lines into documents. Guests convert engine-specific tab columns.
Fixes touching substituted text cannot be
applied to the original expression.

Default exclusions are SC1091, SC2194, SC2050, SC2153, SC2154, SC2157 and SC2043.
The host supplies these numbers; guests do not copy the list. Engine configuration
may add exclusions after preparation. Changing this profile's semantics requires
a new interface/profile version.
