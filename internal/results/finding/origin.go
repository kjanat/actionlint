package finding

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type Owner struct {
	Job  string
	Step *sourcetext.Position
}
