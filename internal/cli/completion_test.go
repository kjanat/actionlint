package cli

import (
	"testing"
)

func TestCompletionShellResolution(t *testing.T) {
	t.Setenv("PATHEXT", "")
	tests := []struct {
		in   string
		want completionShell
		ok   bool
	}{
		{"bash", completionShellBash, true},
		{"fish", completionShellFish, true},
		{"powershell", completionShellPowerShell, true},
		{"zsh", completionShellZsh, true},
		{"pwsh", completionShellPowerShell, true},
		{"/usr/bin/zsh", completionShellZsh, true},
		{"/usr/local/bin/fish", completionShellFish, true},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, completionShellPowerShell, true},
		{"C:/Program Files/PowerShell/7/pwsh.exe", completionShellPowerShell, true},
		{`C:\Program Files/PowerShell\7/pwsh.exe`, completionShellPowerShell, true},
		{"C:/tools/PwSh.ExE", completionShellPowerShell, true},
		{"//server/share/PowerShell/pwsh.exe", completionShellPowerShell, true},
		{`\\server/share\PowerShell/pwsh.exe`, completionShellPowerShell, true},
		{`C:\tools\pwsh.CMD`, completionShellPowerShell, true},
		{`C:\tools\powershell.bat`, completionShellPowerShell, true},
		{`C:\tools\pwsh.com`, completionShellPowerShell, true},
		{"ZSH", completionShellZsh, true},
		{"tcsh", "", false},
		{"/usr/bin/ksh", "", false},
		{"", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			have, ok := completionShellFromPath(tc.in)
			if ok != tc.ok || have != tc.want {
				t.Fatalf("completionShellFromPath(%q) = (%q, %v) but wanted (%q, %v)", tc.in, have, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCompletionShellPathExt(t *testing.T) {
	for _, tc := range []struct {
		name, pathExt, input string
		want                 completionShell
		ok                   bool
	}{
		{"custom extension", ".EXE;.PS1", `C:\tools\pwsh.PS1`, completionShellPowerShell, true},
		{"mixed case", ".eXe;.pS1", `C:\tools\PwSh.Ps1`, completionShellPowerShell, true},
		{"forward slash custom extension", ".eXe;.pS1", "C:/tools/PwSh.Ps1", completionShellPowerShell, true},
		{"mixed separator custom extension", ".eXe;.pS1", `C:\tools/wrappers\PwSh.Ps1`, completionShellPowerShell, true},
		{"spaces and optional dot", " exe ; pS1 ; .CUSTOM ", `C:\tools\pwsh.CUSTOM`, completionShellPowerShell, true},
		{"excluded exe", ".PS1", `C:\tools\pwsh.exe`, "", false},
		{"excluded cmd", ".PS1", `C:\tools\pwsh.cmd`, "", false},
		{"empty entries", ";;.PS1;;", "powershell.ps1", completionShellPowerShell, true},
		{"no extensions", ";;", "pwsh.exe", "", false},
		{"bare shell", ";;", "pwsh", completionShellPowerShell, true},
		{"unlisted extension", ".EXE", "pwsh.conf", "", false},
		{"unknown shell", ".PS1", "script.ps1", "", false},
		{"one suffix only", ".EXE;.PS1", "pwsh.ps1.exe", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATHEXT", tc.pathExt)
			if got, ok := completionShellFromPath(tc.input); got != tc.want || ok != tc.ok {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCompletionShellDetection(t *testing.T) {
	tests := []struct {
		what              string
		shellVar          string
		powerShellPresent bool
		want              completionShell
		ok                bool
	}{
		{"shell variable wins", "/bin/bash", false, completionShellBash, true},
		{"shell variable wins over the fallback", "/bin/bash", true, completionShellBash, true},
		{"psmodulepath fallback without shell variable", "", true, completionShellPowerShell, true},
		{"psmodulepath fallback on an unsupported shell", "/usr/bin/ksh", true, completionShellPowerShell, true},
		{"no signal", "", false, "", false},
		{"unsupported shell without fallback", "/usr/bin/ksh", false, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have, ok := detectCompletionShell(tc.shellVar, tc.powerShellPresent)
			if ok != tc.ok || have != tc.want {
				t.Fatalf("detectCompletionShell(%q, %v) = (%q, %v) but wanted (%q, %v)", tc.shellVar, tc.powerShellPresent, have, ok, tc.want, tc.ok)
			}
		})
	}
}
