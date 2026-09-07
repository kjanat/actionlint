package actionlint

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRuleActionSelfRepositoryUsesLocalSpec(t *testing.T) {
	tests := []struct {
		spec string
		want string
		ok   bool
	}{
		{"$/a", "./a", true},
		{"$/path/to/action", "./path/to/action", true},
		{"$//a", "./a", true},
		{"$///a", "./a", true},
		{"$/", "", false},
		{"$//", "", false},
		{"$///", "", false},
		{"./a", "", false},
		{"$", "", false},
		{"", "", false},
		{"owner/repo@v1", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			have, ok := selfRepositoryUsesLocalSpec(tc.spec)
			if ok != tc.ok {
				t.Fatalf("wanted ok=%v but have ok=%v for %q", tc.ok, ok, tc.spec)
			}
			if have != tc.want {
				t.Fatalf("wanted %q but have %q for %q", tc.want, have, tc.spec)
			}
		})
	}
}

func TestCompositeStepUnavailableContexts(t *testing.T) {
	tests := []struct {
		what string
		expr string
		bare bool
		want []string
	}{
		{"secrets in interpolation", "echo ${{ secrets.TOKEN }}", false, []string{"secrets"}},
		{"vars in interpolation", "echo ${{ vars.FLAG }}", false, []string{"vars"}},
		{"needs in interpolation", "${{ needs.build.outputs.x }}", false, []string{"needs"}},
		{"bare if with secrets", "secrets.TOKEN != ''", true, []string{"secrets"}},
		{"bare if with vars", "vars.FLAG == 'true'", true, []string{"vars"}},
		{"multiple distinct contexts", "${{ secrets.A }}${{ vars.B }}", false, []string{"secrets", "vars"}},
		{"same context twice is deduped", "${{ secrets.A }}${{ secrets.B }}", false, []string{"secrets"}},
		{"allowed contexts are clean", "${{ github.sha }} ${{ inputs.x }} ${{ steps.a.outputs.b }} ${{ env.E }} ${{ runner.os }} ${{ job.status }} ${{ matrix.m }} ${{ strategy.job-index }}", false, nil},
		{"no expression at all", "echo hello secrets.TOKEN", false, nil},
		{"bare literal", "true", true, nil},
		{"parse error is ignored", "echo ${{ }}", false, nil},
		{"status function is not reported", "always()", true, nil},
		{"unterminated expression", "echo ${{ secrets.A", false, nil},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			got := compositeStepUnavailableContexts(tc.expr, tc.bare)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
