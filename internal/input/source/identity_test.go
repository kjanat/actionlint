package source

import "testing"

func TestIdentityAcrossReorderingAndRevision(t *testing.T) {
	t.Parallel()
	first := NewReferenceIndex("workflow.yml", []byte("one"))
	a := first.Ref("build", "", "first", "steps[0]")
	b := first.Ref("build", "named", "", "steps[1]")
	next := NewReferenceIndex("workflow.yml", []byte("two"))
	b2 := next.Ref("build", "named", "edited", "steps[0]")
	a2 := next.Ref("build", "", "first", "steps[1]")
	if a.Node != a2.Node || b.Node != b2.Node || a.Revision == a2.Revision || a.Address == a2.Address {
		t.Fatalf("identity drift: %+v %+v", a, a2)
	}
	duplicate := next.Ref("build", "", "first", "steps[2]")
	if duplicate.Node == a2.Node {
		t.Fatal("duplicate sibling collision")
	}
	other := NewReferenceIndex("other.yml", []byte("two")).Ref("build", "named", "", "steps[0]")
	if other.Node == b2.Node {
		t.Fatal("cross-document collision")
	}
}
