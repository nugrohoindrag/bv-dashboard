// Package phone: normalisasi nomor telepon/WhatsApp Indonesia ke format internasional tanpa '+' (PRD P3 v2.1 P3-WAM-04).
package phone

import (
	"strings"
	"unicode"
)

// Normalize: nomor lokal Indonesia → format internasional tanpa '+' (P3-WAM-04). "0812-3456 7890" → "6281234567890".
// ok=false bila kosong / terlalu pendek / terlalu panjang.
func Normalize(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case d == "":
		return "", false
	case strings.HasPrefix(d, "0"):
		d = "62" + strings.TrimLeft(d, "0")
	case strings.HasPrefix(d, "8"):
		d = "62" + d
	}
	if len(d) < 10 || len(d) > 15 || strings.HasPrefix(d, "0") {
		return d, false
	}
	// nomor Indonesia: setelah 62 harus diawali 8 (seluler) — nomor rumah tidak memakai WhatsApp
	if strings.HasPrefix(d, "62") && !strings.HasPrefix(d, "628") {
		return d, false
	}
	return d, true
}
