// Package freshness provides cache age classification and freshness metadata.
package freshness

import (
	"fmt"
	"time"
)

// HumanAge returns a human-readable age string for a duration.
func HumanAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		m := int(age.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case age < 24*time.Hour:
		h := int(age.Hours())
		return fmt.Sprintf("%dh ago", h)
	case age < 30*24*time.Hour:
		d := int(age.Hours() / 24)
		return fmt.Sprintf("%dd ago", d)
	default:
		mo := int(age.Hours() / 24 / 30)
		return fmt.Sprintf("%dmo ago", mo)
	}
}
