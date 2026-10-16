package corewasm

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolJSONDecoding(t *testing.T) {
	engine, err := Load(t.Context(), filepath.Join("testdata", "wat", "json-protocol.wasm"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})

	for _, tc := range []struct {
		name     string
		metadata string
		response string
		want     string
	}{
		{
			name:     "unknown fields remain extensible",
			metadata: `{"abi_version":1,"future":{"enabled":true}}`,
			response: `{"ok":true,"future":{"enabled":true}}`,
		},
		{
			name:     "duplicate keys",
			metadata: `{"abi_version":2,"abi_version":1}`,
			response: `{"ok":false,"ok":true}`,
			want:     "duplicate object member name",
		},
		{
			name:     "escaped duplicate keys",
			metadata: `{"abi_version":2,"abi_\u0076ersion":1}`,
			response: `{"ok":false,"\u006fk":true}`,
			want:     "duplicate object member name",
		},
		{
			name:     "duplicate keys in unknown fields",
			metadata: `{"abi_version":1,"future":{"a":1,"a":2}}`,
			response: `{"ok":true,"future":{"a":1,"a":2}}`,
			want:     "duplicate object member name",
		},
		{
			name:     "invalid UTF-8",
			metadata: "{\"abi_version\":1,\"id\":\"\xff\"}",
			response: "{\"ok\":true,\"errors\":[\"\xff\"]}",
			want:     "invalid UTF-8",
		},
		{
			name:     "trailing value",
			metadata: `{"abi_version":1} {}`,
			response: `{"ok":true} {}`,
			want:     "after top-level value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for boundary, err := range map[string]error{
				"metadata": engine.negotiate([]byte(tc.metadata)),
				"response": engine.ValidateConfig(t.Context(), json.RawMessage(tc.response)),
			} {
				if tc.want == "" {
					if err != nil {
						t.Errorf("%s: %v", boundary, err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("%s: error=%v, want %q", boundary, err, tc.want)
				}
			}
		})
	}

	t.Run("case-sensitive protocol fields", func(t *testing.T) {
		if err := engine.negotiate([]byte(`{"ABI_VERSION":1}`)); err == nil || !strings.Contains(err.Error(), "metadata reports ABI version 0") {
			t.Fatalf("metadata accepted incorrectly cased ABI field: %v", err)
		}
		if err := engine.ValidateConfig(t.Context(), json.RawMessage(`{"OK":true}`)); err == nil || !strings.Contains(err.Error(), "reported a failure") {
			t.Fatalf("response accepted incorrectly cased success field: %v", err)
		}
	})
	t.Run("duplicate schema members", func(t *testing.T) {
		if err := engine.negotiate([]byte(`{"abi_version":1,"config_schema":{"type":"object","type":"array"}}`)); err == nil || !strings.Contains(err.Error(), "duplicate object member name") {
			t.Fatalf("metadata accepted duplicate keys inside raw schema: %v", err)
		}
	})
}
