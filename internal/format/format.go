// Package format formats numbers for the site's pages and messages.
package format

import (
	"fmt"
)

// Commas formats a number with thousands separators, e.g. 6000 as "6,000".
func Commas(n int) string {
	digits := fmt.Sprint(n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}
