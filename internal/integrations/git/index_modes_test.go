package git_test

import (
	"testing"

	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"
)

func TestGitIndexModes(t *testing.T) {
	t.Parallel()
	modes, err := gitclient.ParseIndexModes("100644 abc 0\tspace name.sh\x00100755 def 0\tok.sh\x00120000 abc 0\tlink\x00100644 abc 1\tconflict.sh\x00100755 def 2\tconflict.sh\x00")
	if err != nil || modes["space name.sh"] != "100644" || modes["ok.sh"] != "100755" || modes["link"] != "120000" || modes["conflict.sh"] != "unmerged" {
		t.Fatalf("incorrect index modes: %v, %v", modes, err)
	}
}
