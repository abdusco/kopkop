package templates

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// strftime formats t using chrono/strftime-style directives, as used by the
// date filter. Literal text is copied verbatim; unknown directives are kept as-is.
// A "-" flag after "%" removes padding (for example "%-d").
func strftime(t time.Time, format string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			b.WriteByte(c)
			continue
		}
		i++
		noPad := false
		if format[i] == '-' && i+1 < len(format) {
			noPad = true
			i++
		}
		directive := format[i]
		out, ok := strftimeDirective(t, directive, noPad)
		if !ok {
			b.WriteByte('%')
			if noPad {
				b.WriteByte('-')
			}
			b.WriteByte(directive)
			continue
		}
		b.WriteString(out)
	}
	return b.String()
}

func strftimeDirective(t time.Time, d byte, noPad bool) (string, bool) {
	num := func(v, width int, pad byte) string {
		s := strconv.Itoa(v)
		if noPad {
			return s
		}
		for len(s) < width {
			s = string(pad) + s
		}
		return s
	}
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	switch d {
	case 'Y':
		return strconv.Itoa(t.Year()), true
	case 'C':
		return num(t.Year()/100, 2, '0'), true
	case 'y':
		return num(t.Year()%100, 2, '0'), true
	case 'm':
		return num(int(t.Month()), 2, '0'), true
	case 'd':
		return num(t.Day(), 2, '0'), true
	case 'e':
		return num(t.Day(), 2, ' '), true
	case 'H':
		return num(t.Hour(), 2, '0'), true
	case 'k':
		return num(t.Hour(), 2, ' '), true
	case 'I':
		return num(hour12, 2, '0'), true
	case 'l':
		return num(hour12, 2, ' '), true
	case 'M':
		return num(t.Minute(), 2, '0'), true
	case 'S':
		return num(t.Second(), 2, '0'), true
	case 'j':
		return num(t.YearDay(), 3, '0'), true
	case 'u':
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		return strconv.Itoa(wd), true
	case 'w':
		return strconv.Itoa(int(t.Weekday())), true
	case 'B':
		return t.Month().String(), true
	case 'b', 'h':
		return t.Month().String()[:3], true
	case 'A':
		return t.Weekday().String(), true
	case 'a':
		return t.Weekday().String()[:3], true
	case 'p':
		if t.Hour() < 12 {
			return "AM", true
		}
		return "PM", true
	case 'P':
		if t.Hour() < 12 {
			return "am", true
		}
		return "pm", true
	case 'Z':
		name, _ := t.Zone()
		return name, true
	case 'z':
		return t.Format("-0700"), true
	case 's':
		return strconv.FormatInt(t.Unix(), 10), true
	case 'f':
		return fmt.Sprintf("%09d", t.Nanosecond()), true
	case 'F':
		return strftime(t, "%Y-%m-%d"), true
	case 'T':
		return strftime(t, "%H:%M:%S"), true
	case 'R':
		return strftime(t, "%H:%M"), true
	case 'D':
		return strftime(t, "%m/%d/%y"), true
	case 'c':
		return strftime(t, "%a %b %e %H:%M:%S %Y"), true
	case 'x':
		return strftime(t, "%m/%d/%y"), true
	case 'X':
		return strftime(t, "%H:%M:%S"), true
	case 'n':
		return "\n", true
	case 't':
		return "\t", true
	case '%':
		return "%", true
	}
	return "", false
}
