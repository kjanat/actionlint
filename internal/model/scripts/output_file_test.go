package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

func TestNamedFileCommandFormat(t *testing.T) {
	t.Parallel()
	// Reference: actions/runner 1ba885c2065fd9be317808ab3fd406f392635cf7,
	// EnvFileKeyValuePairs in src/Runner.Worker/FileCommandManager.cs.
	for _, tc := range []struct {
		name, text string
		invalid    bool
	}{
		{"empty", "", false},
		{"blank lines", "\n\n", false},
		{"assignment", "available=true\n", false},
		{"unterminated assignment", "available=true", false},
		{"empty assignment name accepted by runner", "=value\n", false},
		{"multiline JSON", "available<<EOF\n{\n  \"available\": true\n}\nEOF\n", false},
		{"delimiter without final newline", "available<<EOF\nvalue\nEOF", false},
		{"CRLF multiline", "available<<EOF\r\nvalue\r\nEOF\r\n", false},
		{"equals before delimiter", "available=a<<b\n", false},
		{"delimiter containing equals", "available<<E=OF\nvalue\nE=OF\n", false},
		{"raw JSON", "{\n  \"available\": true\n}\n", true},
		{"space only", " \n", true},
		{"missing delimiter", "available<<EOF\nvalue\n", true},
		{"empty delimiter", "available<<\n", true},
		{"empty multiline name", "<<EOF\nvalue\nEOF\n", true},
		{"invalid record after multiline", "available<<EOF\nvalue\nEOF\n{\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if problem := scriptmodel.NamedFileCommandError(tc.text); (problem != "") != tc.invalid {
				t.Fatalf("format error = %q, want invalid=%t", problem, tc.invalid)
			}
		})
	}
}

func TestKnownOutputFile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script, want string
		unknown            bool
	}{
		{"original tee heredoc", "tee <<EOF2 >\"${GITHUB_OUTPUT}\"\n{\n  \"available\": true\n}\nEOF2\n", "{\n  \"available\": true\n}\n", false},
		{"echo assignment", `echo 'available=true' >> "$GITHUB_OUTPUT"`, "available=true\n", false},
		{"partial writes", "printf 'available=' >> \"$GITHUB_OUTPUT\"\nprintf '%s\\n' true >> \"$GITHUB_OUTPUT\"", "available=true\n", false},
		{"overwrite", "echo '{' > \"$GITHUB_OUTPUT\"\necho 'available=true' > \"$GITHUB_OUTPUT\"", "available=true\n", false},
		{"multiline group", "{ echo 'available<<EOF'; echo '{'; echo '}'; echo EOF; } >> \"$GITHUB_OUTPUT\"", "available<<EOF\n{\n}\nEOF\n", false},
		{"quoted heredoc", "cat <<'EOF' >> \"$GITHUB_OUTPUT\"\navailable=$VALUE\\literal\nEOF\n", "available=$VALUE\\literal\n", false},
		{"tab stripped heredoc", "cat <<-'EOF' >> \"$GITHUB_OUTPUT\"\n\tavailable=true\n\tEOF\n", "available=true\n", false},
		{"tee argument", "tee \"$GITHUB_OUTPUT\" <<'EOF'\n{\n}\nEOF\n", "{\n}\n", false},
		{"tee pipeline", `printf '%s\n' 'available=true' | tee -a "$GITHUB_OUTPUT"`, "available=true\n", false},
		{"unquoted dynamic heredoc", "cat <<EOF >> \"$GITHUB_OUTPUT\"\n$VALUE\nEOF\n", "", true},
		{"escaped heredoc delimiter", "cat <<\\EOF >> \"$GITHUB_OUTPUT\"\n$VALUE\nEOF\n", "$VALUE\n", false},
		{"dynamic output", `echo "$VALUE" >> "$GITHUB_OUTPUT"`, "", true},
		{"unknown prefix", "generate >> \"$GITHUB_OUTPUT\"\necho '{' >> \"$GITHUB_OUTPUT\"", "", true},
		{"unknown suffix", "echo '{' > \"$GITHUB_OUTPUT\"\ngenerate", "", true},
		{"unknown followed by overwrite", "generate\necho '{' > \"$GITHUB_OUTPUT\"", "", true},
		{"shadowed tee", "tee() { echo 'available=true'; }\ntee <<EOF > \"$GITHUB_OUTPUT\"\n{\nEOF\n", "", true},
		{"alias", "alias echo=true\necho '{' > \"$GITHUB_OUTPUT\"", "", true},
		{"reassigned destination", "GITHUB_OUTPUT=elsewhere\necho '{' > \"$GITHUB_OUTPUT\"", "", true},
		{"expression injection", "echo '${{ inputs.value }}' > \"$GITHUB_OUTPUT\"", "", true},
		{"parameter operation", `echo '{' > "${GITHUB_OUTPUT:-other}"`, "", true},
		{"unquoted destination", `echo '{' > $GITHUB_OUTPUT`, "", true},
		{"other file", `echo '{' > output.json`, "", true},
		{"conditional", `if true; then echo '{' > "$GITHUB_OUTPUT"; fi`, "", true},
		{"background", `echo '{' > "$GITHUB_OUTPUT" &`, "", true},
		{"stdin unknown", `cat > "$GITHUB_OUTPUT"`, "", true},
		{"tee stdin unknown", `tee "$GITHUB_OUTPUT"`, "", true},
		{"echo option", `echo -e '{' > "$GITHUB_OUTPUT"`, "", true},
		{"unsupported printf", `printf '%b' '{' > "$GITHUB_OUTPUT"`, "", true},
		{"stdin consumed by group", "{ cat; cat; } <<EOF > \"$GITHUB_OUTPUT\"\n{\nEOF\n", "", true},
		{"group inner writes", `{ echo '{' > "$GITHUB_OUTPUT"; } >> "$GITHUB_OUTPUT"`, "", true},
		{"pipeline concurrent writes", `echo '{' > "$GITHUB_OUTPUT" | tee -a "$GITHUB_OUTPUT"`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, shell := range []string{"bash", "sh"} {
				got, known := scriptmodel.KnownOutputFile(tc.script, shell)
				if known == tc.unknown || known && got.Text != tc.want {
					t.Fatalf("%s result = %+v, known=%t; want %q, unknown=%t", shell, got, known, tc.want, tc.unknown)
				}
				if known && !strings.Contains(tc.script[got.Start:got.End], "GITHUB_OUTPUT") {
					t.Fatalf("destination range = %q", tc.script[got.Start:got.End])
				}
			}
		})
	}
	for _, shell := range []string{"", "pwsh", "python", "custom bash {0}", "bash -c echo {0}"} {
		if _, known := scriptmodel.KnownOutputFile(`echo '{' > "$GITHUB_OUTPUT"`, shell); known {
			t.Errorf("unsupported shell %q produced known output", shell)
		}
	}
}

func TestOutputFileOriginalTeeWithBash(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash is not installed")
	}
	// Run the output-writing step from PR comment 6710252262 in Bash to check
	// the file contents. The pinned runner source above establishes its rejection.
	const script = "tee <<EOF2 >\"${GITHUB_OUTPUT}\"\n{\n  \"available\": true\n}\nEOF2\n"
	directory := t.TempDir()
	command := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-c", script)
	command.Dir = directory
	command.Env = append(os.Environ(), "GITHUB_OUTPUT=output.txt")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Bash failed: %v\n%s", err, output)
	}
	actual, err := os.ReadFile(filepath.Join(directory, "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, known := scriptmodel.KnownOutputFile(script, "bash")
	if !known || got.Text != string(actual) {
		t.Fatalf("model = %+v, known=%t; Bash wrote %q", got, known, actual)
	}
	if problem := scriptmodel.NamedFileCommandError(string(actual)); problem == "" {
		t.Fatal("raw JSON unexpectedly passed named-output format validation")
	}
}
