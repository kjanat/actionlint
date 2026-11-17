package execution

import scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

// ScriptExecution records the shell and working directory resolved for a run
// step. Only a resolved, existing directory is known.
func ScriptExecution(shell Shell, directory Directory) *scriptmodel.Execution {
	return &scriptmodel.Execution{
		Shell: shell.Name, ShellKnown: shell.Name != "", ShellImplicit: shell.Implicit,
		Directory: directory.Path, DirectoryKnown: directory.Kind == DirectoryKnown,
	}
}
