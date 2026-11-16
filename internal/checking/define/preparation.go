package define

// InputPreparation distinguishes supplied facts from knowledge that could not
// be acquired. Operation failures use Prepare's error result instead.
type InputPreparation interface {
	inputPreparation()
}

// PreparedInputs supplies the constructor's inputs after successful preparation.
type PreparedInputs struct {
	RuleInputs RuleInputs
}

// UnavailableInputs prevents execution when required knowledge is incomplete.
// Reason explains which knowledge could not be determined.
type UnavailableInputs struct {
	Reason string
}

func (PreparedInputs) inputPreparation()    {}
func (UnavailableInputs) inputPreparation() {}
