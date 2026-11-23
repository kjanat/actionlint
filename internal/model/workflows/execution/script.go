package execution

import "actionlint.kjanat.dev/v2/internal/model/scripts"

// ScriptExecution treats only a resolved, existing directory as known.
func ScriptExecution(shell scripts.Shell, directory Directory) *scripts.Execution {
	return &scripts.Execution{
		Shell:     shell,
		Directory: directory.Path, DirectoryKnown: directory.Kind == DirectoryKnown,
	}
}
