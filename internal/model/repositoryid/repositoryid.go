// Package repositoryid models a hosting platform's numeric repository ID, which a lookup may not know.
package repositoryid

import (
	"errors"
	"strconv"
)

type ID struct {
	value uint64
	known bool
}

func Known(value uint64) (ID, error) {
	if value == 0 {
		return ID{}, errors.New("repository ID must be positive")
	}
	return ID{value: value, known: true}, nil
}

func (id ID) Value() (uint64, bool) { return id.value, id.known }

func (id ID) IsZero() bool { return !id.known }

func (id ID) Conflicts(other ID) bool {
	return id.known && other.known && id.value != other.value
}

func (id ID) MarshalJSON() ([]byte, error) {
	if !id.known {
		return nil, errors.New("repository ID is unknown")
	}
	return strconv.AppendUint(nil, id.value, 10), nil
}

func (id *ID) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseUint(string(data), 10, 64)
	if err != nil {
		return errors.New("repository ID must be a positive integer")
	}
	known, err := Known(value)
	if err != nil {
		return err
	}
	*id = known
	return nil
}
