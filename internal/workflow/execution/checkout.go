package execution

import (
	"path"
	"strings"
)

// CheckoutPlacement tracks overlapping checkouts; newer placements shadow older ones.
// Unrelated copies survive.
// Immutable links let conditional composite invocations restore the whole state.
type CheckoutPlacement struct {
	Directory       Directory
	Previous        *CheckoutPlacement
	CaseInsensitive bool
	Foreign         bool
	Redirected      bool
}

func (placement *CheckoutPlacement) Relative(local string) (string, bool) {
	local, prefix := path.Clean(local), path.Clean(placement.Directory.Path)
	if prefix == "." {
		return local, true
	}
	if len(local) < len(prefix) {
		return "", false
	}
	match := local[:len(prefix)] == prefix
	if !match && placement.CaseInsensitive && ASCIIPath(prefix) && ASCIIPath(local[:len(prefix)]) {
		match = strings.EqualFold(local[:len(prefix)], prefix)
	}
	if !match {
		return "", false
	}
	if len(local) == len(prefix) {
		return ".", true
	}
	return strings.CutPrefix(local[len(prefix):], "/")
}

func (placement *CheckoutPlacement) Matching(local string) *CheckoutPlacement {
	for current := placement; current != nil; current = current.Previous {
		if _, matches := current.Relative(local); matches {
			return current
		}
	}
	return nil
}

// MergeCheckoutBranches merges checkout placements from conditional execution.
func MergeCheckoutBranches(before, after *CheckoutPlacement, mayFail bool) *CheckoutPlacement {
	if after == before || after == nil {
		return before
	}
	merged := *after
	merged.Previous = MergeCheckoutBranches(before, after.Previous, mayFail)
	if mayFail || merged.Directory.Kind != DirectoryKnown || merged.Foreign || !merged.Previous.SameSelfCheckout(merged.Directory.Path) {
		merged.Directory.Kind = DirectoryUnknown
		merged.Foreign = false
	}
	return &merged
}

func (placement *CheckoutPlacement) SameSelfCheckout(destination string) bool {
	prior := placement.Matching(destination)
	if prior == nil || prior.Directory.Kind != DirectoryKnown || prior.Foreign || prior.Redirected {
		return false
	}
	relative, matches := prior.Relative(destination)
	return matches && relative == "."
}
