package rule

import "testing"

func TestDescriptorCloneOwnsInputRequirements(t *testing.T) {
	t.Parallel()
	original := Descriptor{Name: "example", RequiredInputs: []InputKind{Scripts, Invocations}}
	cloned := original.Clone()
	cloned.RequiredInputs[0] = Source
	cloned.RequiredInputs = append(cloned.RequiredInputs, Dependencies)
	if original.RequiredInputs[0] != Scripts || len(original.RequiredInputs) != 2 {
		t.Fatalf("cloning shared input requirements: %+v", original.RequiredInputs)
	}
}
