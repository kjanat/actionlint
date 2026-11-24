package component

import (
	"runtime/debug"
	"testing"
)

func TestAdmissionIdentityFollowsHostBuild(t *testing.T) {
	t.Parallel()
	build := func(edit func(*debug.BuildInfo)) string {
		info := &debug.BuildInfo{
			Main:     debug.Module{Path: "actionlint.kjanat.dev/v2", Version: "v2.0.0", Sum: "h1:main"},
			Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "aaaa"}, {Key: "vcs.modified", Value: "false"}, {Key: "GOOS", Value: "linux"}},
			Deps: []*debug.Module{
				{Path: "github.com/kjanat/wacogo", Version: "v0.3.8", Sum: "h1:wacogo"},
				{Path: "github.com/kjanat/wazero", Version: "v1.12.2", Sum: "h1:wazero"},
				{Path: "golang.org/x/text", Version: "v0.44.0", Sum: "h1:text"},
			},
		}
		if edit != nil {
			edit(info)
		}
		return admissionIdentity(info)
	}
	base := build(nil)
	if base != build(nil) {
		t.Fatal("identity is not deterministic")
	}
	if base == admissionIdentity(nil) {
		t.Fatal("build information does not reach the identity")
	}
	for name, edit := range map[string]func(*debug.BuildInfo){
		"module version": func(info *debug.BuildInfo) { info.Main.Version = "v2.0.1" },
		"vcs revision":   func(info *debug.BuildInfo) { info.Settings[0].Value = "bbbb" },
		"vcs modified":   func(info *debug.BuildInfo) { info.Settings[1].Value = "true" },
		"wacogo":         func(info *debug.BuildInfo) { info.Deps[0].Version = "v0.3.9" },
		"wazero":         func(info *debug.BuildInfo) { info.Deps[1].Sum = "h1:other" },
		"replaced wacogo": func(info *debug.BuildInfo) {
			info.Deps[0].Replace = &debug.Module{Path: "../wacogo"}
		},
	} {
		if build(edit) == base {
			t.Errorf("%s does not change the identity", name)
		}
	}
	for name, edit := range map[string]func(*debug.BuildInfo){
		"unrelated setting":    func(info *debug.BuildInfo) { info.Settings[2].Value = "darwin" },
		"unrelated dependency": func(info *debug.BuildInfo) { info.Deps[2].Version = "v0.45.0" },
	} {
		if build(edit) != base {
			t.Errorf("%s changes the identity", name)
		}
	}
}
