package callreferences

import "testing"

func TestParseRepositoryActionFollowsRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec    string
		want    RepositoryTarget
		problem string
	}{
		{"o/r@v", RepositoryTarget{Owner: "o", Repo: "r", Ref: "v"}, ""},
		{"o/r/p/q@v", RepositoryTarget{Owner: "o", Repo: "r", Subpath: "p/q", Ref: "v"}, ""},
		{"o//r@v", RepositoryTarget{Owner: "o", Repo: "r", Ref: "v"}, ""},
		{"/o/r@v", RepositoryTarget{Owner: "o", Repo: "r", Ref: "v"}, ""},
		{"o/r/p/@v", RepositoryTarget{Owner: "o", Repo: "r", Subpath: "p", Ref: "v"}, ""},
		{`o\r\p@v`, RepositoryTarget{Owner: "o", Repo: "r", Subpath: "p", Ref: "v"}, ""},
		{"o/r@feature/x", RepositoryTarget{Owner: "o", Repo: "r", Ref: "feature/x"}, ""},
		{"owner//path@v1", RepositoryTarget{Owner: "owner", Repo: "path", Ref: "v1"}, ""},
		{"./a", RepositoryTarget{}, "ref is missing"},
		{"o/r", RepositoryTarget{}, "ref is missing"},
		{"checkout@v2", RepositoryTarget{}, "owner is missing"},
		{"o/r@v@w", RepositoryTarget{}, `"@" appears more than once`},
		{"o/r@", RepositoryTarget{}, "owner and repo and ref should not be empty"},
		{"o/@v", RepositoryTarget{}, "owner and repo and ref should not be empty"},
		{"/r@v", RepositoryTarget{}, "owner and repo and ref should not be empty"},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			got, problem := ParseRepositoryAction(tc.spec)
			if got != tc.want || problem != tc.problem {
				t.Fatalf("ParseRepositoryAction(%q) = %+v, %q, want %+v, %q", tc.spec, got, problem, tc.want, tc.problem)
			}
		})
	}
}

func TestClassifyAgreesWithRepositoryActionSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spec string
		want UsesReference
	}{
		{"o//r@v", RepositoryReference{Owner: "o", Repo: "r", Ref: "v", HostSource: HostSourceDefault}},
		{`o\r\p@v`, RepositoryReference{Owner: "o", Repo: "r", Subpath: "p", Ref: "v", HostSource: HostSourceDefault}},
		{"self:o/r/p@v", RepositoryReference{Owner: "o", Repo: "r", Subpath: "p", Ref: "v", HostSource: HostSourceSelf}},
		{"o/r@v@w", UnknownReference{}},
		{"o/r/../p@v", UnknownReference{}},
		{"o/r", UnknownReference{}},
	} {
		if got := Classify(tc.spec); got != tc.want {
			t.Errorf("Classify(%q) = %#v, want %#v", tc.spec, got, tc.want)
		}
	}
}
