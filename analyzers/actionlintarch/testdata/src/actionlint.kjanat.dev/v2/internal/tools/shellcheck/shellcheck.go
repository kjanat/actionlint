package shellcheck // want package:"project dependency paths"
import (
	alias "actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/source"
	ctx "context"
)

type Ref = source.Reference

func bad() {
	_ = alias.Runtime{Shell: "sh"} // want "ALA103"
	_ = Ref{Document: "bad"}       // want "ALA103"
	var ref Ref
	ref.Document = "bad"         // want "ALA103"
	_ = ctx.Background()         // want "ALA105"
	_ = ctx.TODO()               // want "ALA105"
	background := ctx.Background // want "ALA105"
	_ = background()
	//nolint:actionlintarch
	_ = alias.Runtime{Shell: "bash"} // want "ALA103"
}
func good(ctx ctx.Context, ref source.Reference) source.Reference { return ref }

func elidedLiterals() {
	_ = []source.Reference{{Document: "bad"}}                 // want "ALA103"
	_ = map[string]source.Reference{"bad": {Document: "bad"}} // want "ALA103"
	_ = []*source.Reference{{Document: "bad"}}                // want "ALA103"
}
func mutations(inv *alias.Invocation) {
	inv.Depth++    // want "ALA103"
	inv.Depth--    // want "ALA103"
	(inv.Depth)++  // want "ALA103"
	inv.Depth += 1 // want "ALA103"
}
