package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// WorkflowCommandSyntaxDeclaration describes command-shaped stdout mistakes.
func WorkflowCommandSyntaxDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "workflow-command-syntax", Description: "Report malformed runner commands in literal script output",
		Category: "suspicious", Subcategory: "scripts", Maturity: "nursery", Provider: "actionlint", Recommended: false, DefaultLevel: "warn",
		Explanation: "The runner prints malformed or unknown :: commands as ordinary text instead of creating annotations or applying command effects.\n\n" +
			"This opt-in warning checks literal echo and printf output in Bash or POSIX shells, print calls in Python, and Write-Host, Write-Output or echo calls in PowerShell. " +
			"It joins writes into stdout lines, including stop and resume tokens split across writes. Command processing is evaluated separately for each possible execution path. " +
			"Comments and unused strings do not produce findings.\n\n" +
			"Registered interpreter settings and literal set changes determine errexit. Known failures and exit commands end execution. " +
			"The check handles the errexit exceptions for conditions, negation and AND/OR lists. " +
			"Unsupported commands, dynamic output, loops and complex control flow stop analysis of that path. A pending partial line is checked only at a known end of output. " +
			"Python and PowerShell scripts are skipped entirely when a statement cannot be parsed by the limited literal-output model.\n\n" +
			"The rule is experimental because language coverage is partial and command-shaped text may be an intentional example. " +
			"The syntax and case-insensitive extension list follow actions/runner revision 1ba885c2065fd9be317808ab3fd406f392635cf7, ActionCommand.cs and ActionCommandManager.cs. " +
			"Extension-specific properties are not validated.",
		Help:             "Use ::name::message or ::name key=value::message with a runner command name. Use stop-commands when intentionally printing command examples.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#workflow-command-syntax",
		SupportsActions:  true, Scope: rule.SourceConfiguration, RequiredInputs: []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
		Examples: []rule.Example{rule.WorkflowExample("Close the annotation name with two colons.",
			"    steps:\n      - shell: bash\n        run: echo \"::notice:Up to date\"\n",
			"    steps:\n      - shell: bash\n        run: echo \"::notice::Up to date\"\n")},
	}
}
