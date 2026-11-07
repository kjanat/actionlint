package main

import (
	"go/build"
	"reflect"
	"testing"
)

func TestGuestBuildExcludesNativePackaging(t *testing.T) {
	for _, test := range []struct {
		name, architecture string
		tags               []string
		guest              bool
	}{
		{name: "native Go", architecture: build.Default.GOARCH},
		{name: "TinyGo host architecture", architecture: build.Default.GOARCH, tags: []string{"tinygo"}, guest: true},
		{name: "Wasm architecture", architecture: "wasm", guest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			context := build.Default
			context.GOARCH, context.BuildTags = test.architecture, test.tags
			guest, err := context.MatchFile(".", "main_guest.go")
			if err != nil {
				t.Fatal(err)
			}
			native, err := context.MatchFile(".", "main_native.go")
			if err != nil {
				t.Fatal(err)
			}
			if guest != test.guest || native == test.guest {
				t.Fatalf("guest selected=%t, native selected=%t; want guest=%t", guest, native, test.guest)
			}
		})
	}
}

func TestInsecureDownloadFlags(t *testing.T) {
	for _, test := range []struct {
		source string
		want   []token
	}{
		{"curl --insecure https://example.com", []token{{6, "--insecure"}}},
		{"curl -fsSk https://example.com", []token{{6, "-fsSk"}}},
		{"wget --no-check-certificate https://example.com", []token{{6, "--no-check-certificate"}}},
		{"curl --fail https://example.com", nil},
		{"echo --insecure", nil},
	} {
		t.Run(test.source, func(t *testing.T) {
			if got := insecureFlags(test.source); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("flags=%+v, want %+v", got, test.want)
			}
		})
	}
}
