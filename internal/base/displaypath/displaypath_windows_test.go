package displaypath

import "testing"

func TestFromDirectoryKeepsAbsolutePathOnAnotherDrive(t *testing.T) {
	t.Parallel()
	const directory = `C:\checkout`
	const path = `D:\workflows\ci.yml`
	if got := FromDirectory(directory, path); got != path {
		t.Fatalf("FromDirectory(%q, %q) = %q, want %q", directory, path, got, path)
	}
}
