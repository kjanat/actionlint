package execution

import (
	"path"
	"strings"
)

// Checkout describes one runner placement without linking mutable state.
type Checkout struct {
	Directory       Directory
	CaseInsensitive bool
	Foreign         bool
	Redirected      bool
}

// CheckoutPlacement is an immutable chain handle. Its zero value has no placement.
type CheckoutPlacement struct{ node *checkoutPlacement }

type checkoutPlacement struct {
	Checkout
	previous CheckoutPlacement
}

func NewCheckoutPlacement(checkout Checkout, previous CheckoutPlacement) CheckoutPlacement {
	return CheckoutPlacement{node: &checkoutPlacement{Checkout: checkout, previous: previous}}
}

func (placement CheckoutPlacement) Present() bool { return placement.node != nil }

func (placement CheckoutPlacement) Details() Checkout {
	if !placement.Present() {
		return Checkout{}
	}
	return placement.node.Checkout
}

func (placement CheckoutPlacement) Previous() CheckoutPlacement {
	if !placement.Present() {
		return CheckoutPlacement{}
	}
	return placement.node.previous
}

func (placement CheckoutPlacement) Relative(local string) (string, bool) {
	if !placement.Present() {
		return "", false
	}
	details := placement.Details()
	local, prefix := path.Clean(local), path.Clean(details.Directory.Path)
	if prefix == "." {
		return local, true
	}
	if len(local) < len(prefix) {
		return "", false
	}
	match := local[:len(prefix)] == prefix
	if !match && details.CaseInsensitive && ASCIIPath(prefix) && ASCIIPath(local[:len(prefix)]) {
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

func (placement CheckoutPlacement) Matching(local string) CheckoutPlacement {
	for current := placement; current.Present(); current = current.Previous() {
		if _, matches := current.Relative(local); matches {
			return current
		}
	}
	return CheckoutPlacement{}
}

// MergeCheckoutBranches merges checkout placements from conditional execution.
func MergeCheckoutBranches(before, after CheckoutPlacement, mayFail bool) CheckoutPlacement {
	if after == before || !after.Present() {
		return before
	}
	merged := after.Details()
	previous := MergeCheckoutBranches(before, after.Previous(), mayFail)
	if mayFail || merged.Directory.Kind != DirectoryKnown || merged.Foreign || !previous.SameSelfCheckout(merged.Directory.Path) {
		merged.Directory.Kind = DirectoryUnknown
		merged.Foreign = false
	}
	return NewCheckoutPlacement(merged, previous)
}

func (placement CheckoutPlacement) SameSelfCheckout(destination string) bool {
	prior := placement.Matching(destination)
	details := prior.Details()
	if !prior.Present() || details.Directory.Kind != DirectoryKnown || details.Foreign || details.Redirected {
		return false
	}
	relative, matches := prior.Relative(destination)
	return matches && relative == "."
}
