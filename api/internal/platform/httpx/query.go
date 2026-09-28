package httpx

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/apperr"
)

// ---------- Filter & sort foundation (PRD P0 v2 §17.2–§17.3) ----------

// LocationScopeParams: parameter lokasi yang diterima semua list operasional — paling spesifik menang.
// Semuanya difilter sebagai subtree ltree (building mencakup tower/floor/area/unit di bawahnya).
var LocationScopeParams = []string{"location_id", "unit_id", "area_id", "floor_id", "tower_id", "building_id"}

// QueryLocation mengembalikan lokasi subtree dari location_id | unit_id | area_id | floor_id | tower_id | building_id.
func QueryLocation(r *http.Request) (*uuid.UUID, error) {
	for _, k := range LocationScopeParams {
		id, err := QueryUUID(r, k)
		if err != nil {
			return nil, err
		}
		if id != nil {
			return id, nil
		}
	}
	return nil, nil
}

// Sort: satu kolom sort dengan arah. Field = nama publik (mis. "created_at"); Desc bila diawali "-".
type Sort struct {
	Field string
	Desc  bool
}

// ParseSort memvalidasi ?sort= terhadap daftar field yang diizinkan (map field publik → ekspresi SQL).
// Kosong → def. Field tidak dikenal → 400 (bukan diabaikan diam-diam).
func ParseSort(r *http.Request, allowed map[string]string, def Sort) (Sort, string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("sort"))
	s := def
	if raw != "" {
		s = Sort{Field: strings.TrimPrefix(raw, "-"), Desc: strings.HasPrefix(raw, "-")}
	}
	expr, ok := allowed[s.Field]
	if !ok {
		keys := make([]string, 0, len(allowed))
		for k := range allowed {
			keys = append(keys, k)
		}
		return s, "", apperr.Validation("sort tidak dikenal: "+s.Field).WithField("sort", "gunakan salah satu: "+strings.Join(keys, ", "))
	}
	dir := " ASC"
	if s.Desc {
		dir = " DESC"
	}
	return s, expr + dir, nil
}
