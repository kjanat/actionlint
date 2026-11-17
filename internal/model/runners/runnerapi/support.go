package runnerapi

import (
	"time"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

type Date = lifecycle.Date

func ParseDate(value string) (Date, error) { return lifecycle.ParseDate(value) }
func DateOf(value time.Time) Date          { return lifecycle.DateOf(value) }

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
	Kind          services.ServiceKind `json:"kind"`
	MinVersion    string               `json:"min_version,omitempty"`
	BeforeVersion string               `json:"before_version,omitempty"`
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
