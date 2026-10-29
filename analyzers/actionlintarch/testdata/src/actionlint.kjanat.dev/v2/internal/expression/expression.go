package expression // want package:"project dependency paths"
import (
	_ "actionlint.kjanat.dev/v2/internal/stringutil" // want "ALA101.*stringutil.*config"
	"fmt"
	_ "go/ast" // want "ALA102"
	"net/url"
	_ "os" // want "ALA102"
)

var _ = fmt.Sprint
var _ = url.Parse
