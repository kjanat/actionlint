package workflow

// Scalar tags preserve source type facts independently of the YAML decoder.
const (
	YamlTagStr   = "!!str"
	YamlTagBool  = "!!bool"
	YamlTagInt   = "!!int"
	YamlTagFloat = "!!float"
	YamlTagNull  = "!!null"
)
