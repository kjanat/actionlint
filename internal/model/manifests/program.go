package manifests

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxInlineProgramBytes bounds embedded source before a process is selected.
// The complete JSON manifest remains subject to maxManifestBytes.
const MaxInlineProgramBytes = 512 << 10

var programFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidateProgram checks the program and its sole argument substitution. Version
// probes invoke the entry itself; their literal arguments never run this file.
func (p ProcessSpec) ValidateProgram() error {
	uses := 0
	for _, arg := range p.Args {
		if arg == "{program-file}" {
			uses++
		} else if strings.Contains(arg, "{program-file}") {
			return errors.New("process program-file placeholder must be a whole argument")
		}
	}
	if p.Version != nil {
		for _, arg := range p.Version.Args {
			if strings.Contains(arg, "{program-file}") {
				return errors.New("process.version.args probes the entry directly; program-file belongs only in process.args")
			}
		}
	}
	if p.Program == nil {
		if uses != 0 {
			return errors.New("program-file placeholder requires process.program")
		}
		return nil
	}
	if uses != 1 {
		return errors.New("process.program requires exactly one program-file argument")
	}
	if !programFilename.MatchString(p.Program.Filename) {
		return errors.New("process.program.filename must be a safe basename of at most 128 ASCII characters")
	}
	if len(p.Program.Inline) == 0 || len(p.Program.Inline) > MaxInlineProgramBytes || !utf8.ValidString(p.Program.Inline) {
		return errors.New("process.program.inline must contain 1 to 512 KiB of UTF-8 source")
	}
	return nil
}
