package actioncatalog

import "testing"

func TestDecodeRejectsInvalidCatalogue(t *testing.T) {
	for name, data := range map[string]string{
		"version":               `{"version":2}`,
		"missing-definition":    `{"version":1,"references":{"a/b@v1":"absent"}}`,
		"active-and-retired":    `{"version":1,"references":{"a/b@v1":"d"},"definitions":{"d":{"name":"a"}},"retired":["a/b@v1"]}`,
		"required":              `{"version":1,"definitions":{"d":{"name":"a","required":["missing"]}}}`,
		"deprecated":            `{"version":1,"definitions":{"d":{"name":"a","deprecated":{"missing":""}}}}`,
		"case-collision":        `{"version":1,"definitions":{"d":{"name":"a","inputs":["Token","token"]}}}`,
		"output-case-collision": `{"version":1,"definitions":{"d":{"name":"a","outputs":["Token","token"]}}}`,
		"unknown-field":         `{"version":1,"unknown":true}`,
		"trailing-data":         `{"version":1} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Fatalf("accepted invalid catalogue: %s", data)
			}
		})
	}
}
