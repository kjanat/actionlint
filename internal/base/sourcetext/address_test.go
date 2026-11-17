package sourcetext

import (
	"path/filepath"
	"testing"
)

func TestFileURI(t *testing.T) {
	t.Parallel()
	cases := []struct{ path, want string }{
		{"", ""},
		{"relative/workflow.yml", ""},
	}
	if filepath.Separator == '\\' {
		cases = append(cases, []struct{ path, want string }{
			{`C:\Users\Kaj Kowalski\répo #1\100%.yaml`, "file:///C:/Users/Kaj%20Kowalski/r%C3%A9po%20%231/100%25.yaml"},
			{`\\server\share\folder name\workflow.yml`, "file://server/share/folder%20name/workflow.yml"},
			{`\\?\C:\repo\workflow.yml`, "file:///C:/repo/workflow.yml"},
			{`\\?\UNC\server\share\workflow.yml`, "file://server/share/workflow.yml"},
			{`\\.\NUL`, ""},
			{`\rooted\without\drive.yml`, ""},
		}...)
	} else {
		cases = append(cases, []struct{ path, want string }{
			{"/tmp/répo #1/100%.yaml", "file:///tmp/r%C3%A9po%20%231/100%25.yaml"},
			{"/tmp/a?b\\c.yaml", "file:///tmp/a%3Fb%5Cc.yaml"},
		}...)
	}
	for _, tc := range cases {
		if got := FileURI(tc.path); got != tc.want {
			t.Errorf("FileURI(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
