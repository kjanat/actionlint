package component

import (
	"encoding/json"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
	"github.com/partite-ai/wacogo"
)

func lowerReference(ref source.Reference) *wacogo.ValRecord {
	return wacogo.NewValRecord(field("document", wacogo.ValString(ref.Document)), field("revision", wacogo.ValString(ref.Revision)), field("node", wacogo.ValString(ref.Node)), field("address", wacogo.ValString(ref.Address)))
}
func liftReference(value wacogo.Val) (source.Reference, error) {
	record, err := liftRecord(value, "source reference")
	if err != nil {
		return source.Reference{}, err
	}
	ref := source.Reference{}
	for name, dest := range map[string]*string{"document": &ref.Document, "revision": &ref.Revision, "node": &ref.Node, "address": &ref.Address} {
		value, err := liftString(record, name)
		if err != nil {
			return ref, err
		}
		*dest = value
	}
	return ref, nil
}
func liftRelated(value wacogo.Val) ([]diagnostic.Related, error) {
	list, err := liftList(value, "related locations")
	if err != nil {
		return nil, err
	}
	result := make([]diagnostic.Related, 0, list.Len())
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "related location")
		if err != nil {
			return nil, err
		}
		var related diagnostic.Related
		if related.Message, err = liftString(record, "message"); err != nil {
			return nil, err
		}
		if related.Path, err = liftString(record, "path"); err != nil {
			return nil, err
		}
		if related.Start, err = liftPosition(record.Field("start")); err != nil {
			return nil, err
		}
		if related.End, err = liftPosition(record.Field("end")); err != nil {
			return nil, err
		}
		result = append(result, related)
	}
	return result, nil
}
func liftRevisions(value wacogo.Val) (map[string]string, error) {
	list, err := liftList(value, "source revisions")
	if err != nil {
		return nil, err
	}
	revisions := map[string]string{}
	for i := range list.Len() {
		record, err := liftRecord(list.Get(i), "source revision")
		if err != nil {
			return nil, err
		}
		path, err := liftString(record, "path")
		if err != nil {
			return nil, err
		}
		digest, err := liftString(record, "digest")
		if err != nil {
			return nil, err
		}
		if _, ok := revisions[path]; ok {
			return nil, fmt.Errorf("duplicate source revision %q", path)
		}
		revisions[path] = digest
	}
	return revisions, nil
}

// Keep schema metadata valid before it reaches editors or generated references.
func validOptionsSchema(raw string) (json.RawMessage, error) {
	if raw == "" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, errors.New("plugin options schema is not JSON")
	}
	return json.RawMessage(raw), nil
}
