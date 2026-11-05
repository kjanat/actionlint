package source

// Coordinates identifies the physical coordinate rules for a captured document.
// The zero value is invalid. Availability also requires valid UTF-8 bytes.
type Coordinates string

const (
	// YAML strips a leading BOM and recognizes YAML line breaks.
	YAML Coordinates = "yaml"
	// RawLF recognizes LF only; CR and BOM remain physical scalars.
	RawLF Coordinates = "raw-lf"
)

func (c Coordinates) Valid() bool { return c == YAML || c == RawLF }
