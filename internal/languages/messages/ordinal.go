package messages

import "fmt"

// Ordinal formats a positive argument position, such as 1st, 2nd or 11th.
func Ordinal(i int) string {
	suffix := "th"
	switch i % 10 {
	case 1:
		if i%100 != 11 {
			suffix = "st"
		}
	case 2:
		if i%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if i%100 != 13 {
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", i, suffix)
}
