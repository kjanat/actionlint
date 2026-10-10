package ruff

import "testing"

func TestDecode(t *testing.T) {
	for _, data := range []string{`[]`, `[{"code":"F821","message":"Undefined name","location":{"row":2,"column":3}}]`} {
		if _, err := Decode([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{`null`, `{}`, `[] trailing`, `[{"code":"F821"}]`, `[{"code":"F821","message":"bad","location":{"row":0,"column":3}}]`} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
}

func TestDecodePreviewSyntax(t *testing.T) {
	diagnostics, err := Decode([]byte(`[{"code":null,"name":"invalid-syntax","severity":"error","message":"Expected an expression","location":{"row":1,"column":3}}]`))
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Code != "invalid-syntax" {
		t.Fatalf("preview syntax diagnostic: %+v, %v", diagnostics, err)
	}
	if _, err := Decode([]byte(`[{"code":null,"name":"","message":"Expected an expression","location":{"row":1,"column":3}}]`)); err == nil {
		t.Fatal("accepted diagnostic with neither code nor name")
	}
}
