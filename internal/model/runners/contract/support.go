package runner

import (
	"encoding/json"
	"errors"
	"time"

	"actionlint.kjanat.dev/v2/internal/model/services"
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

type Hosting string

const (
	UnknownHosting Hosting = ""
	Hosted         Hosting = "hosted"
	SelfHosted     Hosting = "self-hosted"
)

type Capability string

const (
	JobExecution           Capability = "job-execution"
	NativeAArch32Userspace Capability = "native-aarch32-userspace"
)

type SupportState string

const (
	SupportUnknown SupportState = ""
	Supported      SupportState = "supported"
	Deprecated     SupportState = "deprecated"
	Unsupported    SupportState = "unsupported"
)

type ServiceScope struct {
	Kind          services.Kind `json:"kind"`
	MinVersion    string        `json:"min_version,omitempty"`
	BeforeVersion string        `json:"before_version,omitempty"`
}
type (
	SupportChange struct {
		On    Date         `json:"on"`
		State SupportState `json:"state"`
	}
	SupportPolicy struct {
		ID            string          `json:"id"`
		Description   string          `json:"description"`
		URL           string          `json:"url"`
		Capability    Capability      `json:"capability"`
		Service       ServiceScope    `json:"service"`
		Hosting       []Hosting       `json:"hosting"`
		OS            []OS            `json:"os"`
		Architectures []string        `json:"architectures"`
		Initial       SupportState    `json:"initial"`
		Changes       []SupportChange `json:"changes"`
	}
)

type (
	SupportCatalog struct {
		DatasetStatus DatasetStatus   `json:"dataset_status"`
		Policies      []SupportPolicy `json:"policies"`
	}
	Response struct {
		Answers []Answer       `json:"answers"`
		Support SupportCatalog `json:"support"`
	}
)
