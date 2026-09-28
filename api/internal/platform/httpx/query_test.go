package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestParseSortAndLocation(t *testing.T) {
	allowed := map[string]string{"created_at": "x.created_at", "priority": "x.prio"}
	r := httptest.NewRequest("GET", "/?sort=-priority", nil)
	s, expr, err := ParseSort(r, allowed, Sort{Field: "created_at", Desc: true})
	if err != nil || s.Field != "priority" || !s.Desc || expr != "x.prio DESC" {
		t.Fatalf("sort: %+v %q %v", s, expr, err)
	}
	r = httptest.NewRequest("GET", "/", nil)
	if _, expr, _ := ParseSort(r, allowed, Sort{Field: "created_at", Desc: true}); expr != "x.created_at DESC" {
		t.Fatalf("default sort: %q", expr)
	}
	r = httptest.NewRequest("GET", "/?sort=drop_table", nil)
	if _, _, err := ParseSort(r, allowed, Sort{Field: "created_at"}); err == nil {
		t.Fatal("sort tidak dikenal harus error")
	}
	// lokasi: paling spesifik menang
	r = httptest.NewRequest("GET", "/?building_id=00000000-0000-0000-0000-000000000001&floor_id=00000000-0000-0000-0000-000000000002", nil)
	id, err := QueryLocation(r)
	if err != nil || id == nil || id.String() != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("location: %v %v", id, err)
	}
	r = httptest.NewRequest("GET", "/?area_id=bukan-uuid", nil)
	if _, err := QueryLocation(r); err == nil {
		t.Fatal("uuid tidak valid harus error")
	}
}
