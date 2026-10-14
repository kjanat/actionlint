package actioncatalog

import "testing"

func TestDecodeRejectsInvalidCatalogue(t *testing.T) {
	for name, data := range map[string]string{
		"version":               `{"version":2,"references":{},"definitions":{},"retired":[]}`,
		"empty":                 `{"version":1}`,
		"missing-references":    `{"version":1,"definitions":{},"retired":[]}`,
		"null-definitions":      `{"version":1,"references":{},"definitions":null,"retired":[]}`,
		"missing-retired":       `{"version":1,"references":{},"definitions":{}}`,
		"missing-definition":    `{"version":1,"references":{"a/b@v1":"absent"},"definitions":{},"retired":[]}`,
		"active-and-retired":    `{"version":1,"references":{"a/b@v1":"d"},"definitions":{"d":{"name":"a"}},"retired":["a/b@v1"]}`,
		"required":              `{"version":1,"references":{},"definitions":{"d":{"name":"a","required":["missing"]}},"retired":[]}`,
		"deprecated":            `{"version":1,"references":{},"definitions":{"d":{"name":"a","deprecated":{"missing":""}}},"retired":[]}`,
		"case-collision":        `{"version":1,"references":{},"definitions":{"d":{"name":"a","inputs":["Token","token"]}},"retired":[]}`,
		"output-case-collision": `{"version":1,"references":{},"definitions":{"d":{"name":"a","outputs":["Token","token"]}},"retired":[]}`,
		"unknown-field":         `{"version":1,"references":{},"definitions":{},"retired":[],"unknown":true}`,
		"trailing-data":         `{"version":1,"references":{},"definitions":{},"retired":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Fatalf("accepted invalid catalogue: %s", data)
			}
		})
	}
}

func TestDecodeAcceptsEmptyCollections(t *testing.T) {
	if _, err := Decode([]byte(`{"version":1,"references":{},"definitions":{},"retired":[]}`)); err != nil {
		t.Fatal(err)
	}
}
