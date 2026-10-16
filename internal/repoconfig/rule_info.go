package repoconfig

type RuleInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Maturity    string `json:"maturity"`
	Recommended bool   `json:"recommended"`
}

// RuleInfos describes checks without constructing their analyzers.
func RuleInfos() []RuleInfo {
	rules := Rules()
	result := make([]RuleInfo, len(rules))
	for i, rule := range rules {
		maturity := "stable"
		if rule.Nursery() {
			maturity = "nursery"
		}
		result[i] = RuleInfo{Name: rule.Name, Description: rule.Description, Category: rule.Category, Maturity: maturity, Recommended: rule.Recommended}
	}
	return result
}
