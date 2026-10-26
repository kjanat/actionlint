package sourceid // want package:"project dependency paths"
import "actionlint.kjanat.dev/v2/source"

func New() source.Reference { return source.Reference{Document: "ok"} }
