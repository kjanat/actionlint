package execution

import "actionlint.kjanat.dev/v2/internal/model/scripts"

// ScriptExecution records the shell and working directory resolved for a run
// step. Only a resolved, existing directory is known.
func ScriptExecution(shell scripts.Shell, directory Directory) *scripts.Execution {
	return &scripts.Execution{
		Shell:     shell,
		Directory: directory.Path, DirectoryKnown: directory.Kind == DirectoryKnown,
	}
}
