package rule

import "slices"

const (
	StableMaturity  = "stable"
	NurseryMaturity = "nursery"
)

// Maturities lists accepted nonempty rule maturity values.
func Maturities() []string {
	return []string{StableMaturity, NurseryMaturity}
}

// ValidMaturity accepts declared maturity or an unspecified value.
func ValidMaturity(maturity string) bool {
	return maturity == "" || slices.Contains(Maturities(), maturity)
}

// Experimental reports whether stable presets exclude this rule.
func (d Descriptor) Experimental() bool {
	return d.Maturity == NurseryMaturity
}
