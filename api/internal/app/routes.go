package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RouteInfo: satu route terdaftar beserta hasil probe autentikasi (PRD P0 v2 §19.2, §29 "API authentication coverage").
type RouteInfo struct {
	Method string
	Route  string
	Status int    // status probe tanpa kredensial
	Code   string // kode problem+json (bila ada)
}

// RequiresStaffAuth: route ditolak middleware IAM Authenticate saat tanpa bearer token.
func (ri RouteInfo) RequiresStaffAuth() bool {
	return ri.Status == http.StatusUnauthorized && ri.Code == "UNAUTHORIZED"
}

// ProbeRoutes menelusuri seluruh route router (chi.Walk) dan memanggil masing-masing tanpa kredensial.
// Parameter path diisi UUID dummy. Dipakai oleh generator OpenAPI (penanda security) dan test cakupan auth.
func ProbeRoutes(router chi.Router) []RouteInfo {
	var out []RouteInfo
	_ = chi.Walk(router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodOptions || method == http.MethodHead {
			return nil
		}
		route = strings.ReplaceAll(route, "/*/", "/")
		path := route
		for {
			i := strings.Index(path, "{")
			if i < 0 {
				break
			}
			j := strings.Index(path[i:], "}")
			if j < 0 {
				break
			}
			path = path[:i] + "00000000-0000-0000-0000-000000000001" + path[i+j+1:]
		}
		path = strings.ReplaceAll(path, "*", "x")
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		func() {
			defer func() { _ = recover() }()
			router.ServeHTTP(rec, req)
		}()
		ri := RouteInfo{Method: method, Route: route, Status: rec.Code}
		var body struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &body) == nil {
			ri.Code = body.Code
			// 401 dari Authenticate IAM (bukan 401 domain seperti refresh token/tanda tangan webhook)
			if rec.Code == http.StatusUnauthorized && !strings.Contains(body.Detail, "bearer token diperlukan") {
				ri.Code = "DOMAIN_401"
			}
		}
		out = append(out, ri)
		return nil
	})
	return out
}
