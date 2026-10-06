package slug

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func Normalize(input string) string {
	s := norm.NFC.String(strings.TrimSpace(strings.ToLower(input)))
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		isAlphaNum := unicode.IsLetter(r) || unicode.IsNumber(r) || (unicode.IsMark(r) && b.Len() > 0 && !lastDash)
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}

		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}
