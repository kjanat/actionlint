package define

// InputPreparation distinguishes supplied facts from knowledge that could not
// be acquired. Prepare's error result reports operation failures.
type InputPreparation interface {
	inputPreparation()
}

type PreparedInputs struct {
	RuleInputs RuleInputs
}

// UnavailableInputs prevents execution when required knowledge is incomplete.
type UnavailableInputs struct {
	Reason string
}

func (PreparedInputs) inputPreparation()    {}
func (UnavailableInputs) inputPreparation() {}
