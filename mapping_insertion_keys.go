package actionlint

import "go.yaml.in/yaml/v4"

func knownInsertedMappingKeys(node *yaml.Node) map[string]*String {
	return inputResolver.InsertedKeys(node, newString, isMappingInsertion)
}

func knownMappingKey(key *String) (*String, bool) {
	return inputResolver.Key(key)
}
