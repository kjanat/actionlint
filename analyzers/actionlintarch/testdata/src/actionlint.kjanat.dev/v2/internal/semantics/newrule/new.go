package newrule // want package:"project dependency paths"
import "actionlint.kjanat.dev/v2/source"

func Document(r source.Reference) string { return r.Document }
