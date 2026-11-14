// Package lifecycle evaluates dated support events against notification policies.
package lifecycle

import (
	"encoding/json"
	"errors"
	"time"
)

type Date struct{ iso string }

func ParseDate(value string) (Date, error) {
	t, err := time.Parse("2006-01-02", value)
	if err != nil || t.Format("2006-01-02") != value {
		return Date{}, errors.New("date must be a valid YYYY-MM-DD")
	}
	return Date{iso: value}, nil
}
func DateOf(value time.Time) Date { d, _ := ParseDate(value.UTC().Format("2006-01-02")); return d }
func (d Date) String() string     { return d.iso }
func (d Date) IsZero() bool       { return d.iso == "" }
func (d Date) Compare(other Date) int {
	if d.iso < other.iso {
		return -1
	}
	if d.iso > other.iso {
		return 1
	}
	return 0
}
func (d Date) MarshalText() ([]byte, error) { return []byte(d.iso), nil }
func (d *Date) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		*d = Date{}
		return nil
	}
	value, err := ParseDate(string(data))
	if err != nil {
		return err
	}
	*d = value
	return nil
}
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.iso) }
func (d *Date) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(value))
}
