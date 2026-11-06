package main

import (
	"reflect"
	"testing"
)

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
