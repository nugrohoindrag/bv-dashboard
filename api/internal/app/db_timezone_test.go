package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/buildingvision/api/internal/platform/db"
)

// Sesi database memakai zona operasional (default Asia/Jakarta) walau server Postgres berjalan di UTC: SQL memakai
// current_date untuk "hari ini" (izin parkir aktif, akses tenant, kedaluwarsa) sehingga pukul 00:00–07:00 WIB tanggal
// UTC server masih kemarin. Zona waktu yang ditulis eksplisit di URL tetap dihormati.
func TestDBSessionTimezone(t *testing.T) {
	t.Setenv("BV_DB_TIMEZONE", "")
	ctx := context.Background()
	aurl := adminURL()
	base := aurl[:strings.LastIndex(aurl, "/")] + "/postgres?sslmode=disable"

	cases := []struct{ url, want string }{
		{base, "Asia/Jakarta"},          // default
		{base + "&timezone=UTC", "UTC"}, // eksplisit di URL menang
	}
	for _, c := range cases {
		d, err := db.Open(ctx, c.url)
		if err != nil {
			t.Fatalf("open %s: %v", c.url, err)
		}
		var tz string
		err = d.Pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz)
		d.Close()
		if err != nil {
			t.Fatalf("query timezone: %v", err)
		}
		if tz != c.want {
			t.Fatalf("timezone sesi %q untuk %s, harus %q", tz, c.url, c.want)
		}
	}

	t.Setenv("BV_DB_TIMEZONE", "Asia/Makassar")
	d, err := db.Open(ctx, base)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	var tz string
	if err := d.Pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz); err != nil || tz != "Asia/Makassar" {
		t.Fatalf("BV_DB_TIMEZONE diabaikan: %q %v", tz, err)
	}
}
