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
