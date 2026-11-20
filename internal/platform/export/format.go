package export

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Lima is UTC-5 all year (no daylight saving time), exact without the tz database.
var Lima = time.FixedZone("America/Lima", -5*60*60)

// DefaultColor is Pulso's colour, used when a gym has none or it is malformed.
const DefaultColor = "#17677b"

// FormatMoney turns "1245.8" into "S/ 1,245.80" (Peruvian style: comma thousands, dot decimals).
// Text that is not a number is returned unchanged.
func FormatMoney(raw string) string {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return raw
	}
	return "S/ " + groupThousands(value, 2)
}

// FormatNumber formats a number with the given decimals and thousands separators ("1,234.5").
func FormatNumber(value float64, decimals int) string { return groupThousands(value, decimals) }

func groupThousands(value float64, decimals int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	whole, fraction, _ := strings.Cut(text, ".")
	var b strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	if fraction != "" {
		b.WriteByte('.')
		b.WriteString(fraction)
	}
	return sign + b.String()
}

// FormatDate turns "2026-11-20" into "20/11/2026"; other text is returned unchanged.
func FormatDate(raw string) string {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return raw
	}
	return t.Format("02/01/2006")
}

// FormatDateTime shows an instant in Lima as "20/11/2026 15:42".
func FormatDateTime(t time.Time) string { return t.In(Lima).Format("02/01/2006 15:04") }

// display is how a value is shown in the PDF.
func display(kind Kind, raw string) string {
	if raw == "" {
		return ""
	}
	switch kind {
	case Money:
		return FormatMoney(raw)
	case Date:
		return FormatDate(raw)
	case Integer:
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return groupThousands(v, 0)
		}
	case Decimal:
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return groupThousands(v, 1)
		}
	}
	return raw
}

// rightAligned says whether a kind is a number.
func rightAligned(k Kind) bool { return k == Money || k == Integer || k == Decimal }

// RGB is a colour.
type RGB struct{ R, G, B int }

// ParseColor reads "#rrggbb", falling back to DefaultColor.
func ParseColor(hex string) RGB {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		hex = strings.TrimPrefix(DefaultColor, "#")
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		v, _ = strconv.ParseUint(strings.TrimPrefix(DefaultColor, "#"), 16, 32)
	}
	return RGB{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}
}

// Hex is "rrggbb" (no #), as Excel wants it.
func (c RGB) Hex() string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, 6)
	for _, v := range []int{c.R, c.G, c.B} {
		out = append(out, digits[v>>4&0xf], digits[v&0xf])
	}
	return string(out)
}

// luminance is the WCAG relative luminance.
func (c RGB) luminance() float64 {
	channel := func(v int) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// OnColor is the text colour readable on top of c: white, or dark ink for a very light brand colour
// (the same rule the web uses for its buttons).
func (c RGB) OnColor() RGB {
	if c.luminance() > 0.179 {
		return RGB{0x14, 0x26, 0x30}
	}
	return RGB{255, 255, 255}
}

// Mix blends c with white; amount 0 is c, 1 is white. Used for soft tints of the brand colour.
func (c RGB) Mix(amount float64) RGB {
	mix := func(v int) int { return int(math.Round(float64(v) + (255-float64(v))*amount)) }
	return RGB{mix(c.R), mix(c.G), mix(c.B)}
}

// Initials are up to two letters of a name, for the monogram when there is no logo.
func Initials(name string) string {
	var out []rune
	for _, word := range strings.Fields(name) {
		first := []rune(word)[0]
		if len(out) < 2 && (unicode.IsLetter(first) || unicode.IsDigit(first)) {
			out = append(out, unicode.ToUpper(first))
		}
	}
	if len(out) == 0 {
		return "P"
	}
	return string(out)
}

// Clean keeps the text the embedded font can draw: control characters become spaces and symbols outside the
// basic planes the font covers (emoji, for instance) are dropped, so user text never breaks a PDF.
func Clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteRune(' ')
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			continue
		case r > 0x2bff:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
