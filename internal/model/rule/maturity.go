package rule

import "slices"

const (
	StableMaturity  = "stable"
	NurseryMaturity = "nursery"
)

func Maturities() []string {
	return []string{StableMaturity, NurseryMaturity}
}

func ValidMaturity(maturity string) bool {
	return maturity == "" || slices.Contains(Maturities(), maturity)
}

// Experimental reports whether stable presets exclude this rule.
func (d Descriptor) Experimental() bool {
	return d.Maturity == NurseryMaturity
}
