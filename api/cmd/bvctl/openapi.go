package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/buildingvision/api/internal/app"
	"github.com/buildingvision/api/internal/asset"
	"github.com/buildingvision/api/internal/attachments"
	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/engineering"
	"github.com/buildingvision/api/internal/exports"
	"github.com/buildingvision/api/internal/housekeeping"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/notification"
	"github.com/buildingvision/api/internal/operations"
	"github.com/buildingvision/api/internal/overview"
	"github.com/buildingvision/api/internal/platform/config"
	"github.com/buildingvision/api/internal/property"
	"github.com/buildingvision/api/internal/search"
	"github.com/buildingvision/api/internal/security"
	bvsync "github.com/buildingvision/api/internal/sync"
	"github.com/buildingvision/api/internal/tenantservice"
)

// runOpenAPI: generate contracts/openapi/v1.yaml dari router (path, method, tag) + skema reflektif dari struct Go.
// TAD ADR-004 menyebut spec-first; P0 memakai code-first dengan spec ter-generate di CI agar tidak menyimpang.
func runOpenAPI(w io.Writer) error {
	cfg, _ := config.Load()
	a, err := app.New(app.Options{Cfg: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		return err
	}
	a.Use(app.DefaultExtensions(a)...)
	router := a.BuildRouter().(chi.Router)

	g := &gen{schemas: map[string]any{}, seen: map[reflect.Type]string{}}
	// register skema utama
	types := map[string]any{
		"WorkItem": operations.WorkItem{}, "CreateTaskInput": operations.CreateTaskInput{}, "CreateWorkOrderInput": operations.CreateWorkOrderInput{},
		"UpdateWorkItemInput": operations.UpdateWorkItemInput{}, "AssignInput": operations.AssignInput{}, "TransitionInput": operations.TransitionInput{},
		"ChecklistTemplate": operations.ChecklistTemplate{}, "ChecklistTemplateInput": operations.ChecklistTemplateInput{}, "ChecklistRun": operations.ChecklistRun{}, "AnswerInput": operations.AnswerInput{},
		"Comment": operations.Comment{}, "ObjectLink": operations.ObjectLink{}, "LinkRef": operations.LinkRef{}, "Assignment": operations.Assignment{},
		"Finding": operations.Finding{}, "CreateFindingInput": operations.CreateFindingInput{}, "Incident": operations.Incident{}, "CreateIncidentInput": operations.CreateIncidentInput{}, "UpdateIncidentInput": operations.UpdateIncidentInput{},
		"SLAPolicy": operations.SLAPolicy{}, "SLAPolicyInput": operations.SLAPolicyInput{},
		"ServiceRequest": tenantservice.ServiceRequest{}, "CreateServiceRequestInput": tenantservice.CreateInput{}, "UpdateServiceRequestInput": tenantservice.UpdateInput{}, "ServiceRequestCategory": tenantservice.Category{},
		"Asset": asset.Asset{}, "AssetInput": asset.AssetInput{}, "Equipment": asset.Equipment{}, "EquipmentInput": asset.EquipmentInput{}, "QRResolve": asset.QRResolve{}, "AssetHistoryItem": asset.HistoryItem{},
		"MaintenancePlan": engineering.MaintenancePlan{}, "MaintenancePlanInput": engineering.PlanInput{}, "MaintenanceSchedule": engineering.Schedule{}, "CreateInspectionInput": engineering.CreateInspectionInput{},
		"Checkpoint": security.Checkpoint{}, "CheckpointInput": security.CheckpointInput{}, "PatrolRoute": security.PatrolRoute{}, "PatrolRouteInput": security.RouteInput{}, "PatrolSchedule": security.PatrolSchedule{}, "PatrolScheduleInput": security.ScheduleInput{}, "CheckpointScan": security.Scan{}, "ScanInput": security.ScanInput{},
		"CleaningSchedule": housekeeping.CleaningSchedule{}, "CleaningScheduleInput": housekeeping.ScheduleInput{}, "CreateHousekeepingInspectionInput": housekeeping.CreateInspectionInput{},
		"Location": property.Location{}, "CreateLocationInput": property.CreateLocationInput{}, "UpdateLocationInput": property.UpdateLocationInput{}, "Tenant": property.Tenant{}, "TenantInput": property.TenantInput{}, "Occupant": property.Occupant{}, "Organization": property.Organization{},
		"User": iam.User{}, "CreateUserInput": iam.CreateUserInput{}, "UpdateUserInput": iam.UpdateUserInput{}, "Role": iam.Role{}, "RoleInput": iam.RoleInput{}, "Team": iam.Team{}, "TeamInput": iam.TeamInput{}, "TokenPair": iam.TokenPair{}, "DeviceInput": iam.DeviceInput{},
		"Attachment": attachments.Attachment{}, "PresignInput": attachments.PresignInput{}, "PresignOutput": attachments.PresignOutput{}, "ConfirmInput": attachments.ConfirmInput{},
		"Activity": audit.Activity{}, "AuditLog": audit.AuditLog{},
		"Notification": notification.Notification{}, "NotificationPreference": notification.Preference{},
		"SearchResult": search.Result{}, "Export": exports.Export{},
		"OverviewToday": overview.Today{}, "AttentionItem": overview.AttentionItem{}, "TodaysOperations": overview.TodaysOperations{}, "WorkloadRow": overview.WorkloadRow{}, "BuildingState": overview.BuildingState{}, "TenantRequestsPanel": overview.TenantRequestsPanel{},
		"SyncBundle": bvsync.Bundle{}, "SyncPushInput": bvsync.PushInput{}, "SyncPushOutput": bvsync.PushOutput{}, "SyncConflict": bvsync.Conflict{},
	}
	names := make([]string, 0, len(types))
	for n := range types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		g.schemaOf(n, reflect.TypeOf(types[n]))
	}
	g.schemas["Problem"] = map[string]any{"type": "object", "description": "RFC 9457 problem+json", "properties": map[string]any{
		"type": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "status": map[string]any{"type": "integer"}, "detail": map[string]any{"type": "string"},
		"code": map[string]any{"type": "string"}, "errors": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"field": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}}}, "request_id": map[string]any{"type": "string"}}}

	paths := map[string]map[string]any{}
	_ = chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == http.MethodOptions || method == http.MethodHead {
			return nil
		}
		route = strings.ReplaceAll(route, "/*/", "/")
		if !strings.HasPrefix(route, "/api/v1") && !strings.HasPrefix(route, "/public") && route != "/health" && route != "/ready" {
			return nil
		}
		op := map[string]any{"tags": []string{tagOf(route)}, "summary": summaryOf(method, route), "responses": responsesFor(method, route)}
		var params []map[string]any
		for _, seg := range strings.Split(route, "/") {
			if strings.HasPrefix(seg, "{") {
				name := strings.Trim(seg, "{}")
				params = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		if method == http.MethodGet && !strings.Contains(route, "{id}") && strings.HasSuffix(route, "s") {
			params = append(params, map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 25, "maximum": 200}},
				map[string]any{"name": "cursor", "in": "query", "schema": map[string]any{"type": "string"}},
				map[string]any{"name": "property_id", "in": "query", "schema": map[string]any{"type": "string", "format": "uuid"}},
				map[string]any{"name": "status", "in": "query", "schema": map[string]any{"type": "string"}, "description": "CSV"},
				map[string]any{"name": "priority", "in": "query", "schema": map[string]any{"type": "string"}, "description": "CSV"},
				map[string]any{"name": "location_id", "in": "query", "schema": map[string]any{"type": "string", "format": "uuid"}, "description": "subtree"},
				map[string]any{"name": "assignee_id", "in": "query", "schema": map[string]any{"type": "string", "format": "uuid"}},
				map[string]any{"name": "team_id", "in": "query", "schema": map[string]any{"type": "string", "format": "uuid"}},
				map[string]any{"name": "q", "in": "query", "schema": map[string]any{"type": "string"}},
				map[string]any{"name": "sort", "in": "query", "schema": map[string]any{"type": "string"}, "description": "mis. -due_at"})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if body := bodyFor(method, route); body != "" {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/" + body}}}}
		}
		if !strings.HasPrefix(route, "/api/v1/auth") && !strings.HasPrefix(route, "/public") && route != "/health" && route != "/ready" {
			op["security"] = []map[string]any{{"bearerAuth": []string{}}}
		}
		if paths[route] == nil {
			paths[route] = map[string]any{}
		}
		paths[route][strings.ToLower(method)] = op
		return nil
	})
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "BuildingVision API", "version": "1.0.0", "description": "REST API BuildingVision P0 — Building Operations Platform. Path /api/v1/{resource} kebab-case; field snake_case; timestamp ISO 8601; error RFC 9457 problem+json; pagination cursor (`?cursor=&limit=`). Header: Authorization Bearer, X-Request-Id, Idempotency-Key (POST), If-Match (version)."},
		"servers": []map[string]any{{"url": "http://localhost:8080"}, {"url": "https://api.buildingvision.id"}},
		"tags":    tags(),
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}},
			"schemas":         g.schemas,
		},
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	fmt.Fprintf(w, "# GENERATED by `bvctl openapi` — %s. Jangan edit manual; ubah handler/struct lalu regenerate (CI memverifikasi).\n", time.Now().UTC().Format("2006-01-02"))
	return enc.Encode(doc)
}

type gen struct {
	schemas map[string]any
	seen    map[reflect.Type]string
}

func (g *gen) schemaOf(name string, t reflect.Type) map[string]any {
	if n, ok := g.seen[t]; ok {
		return map[string]any{"$ref": "#/components/schemas/" + n}
	}
	g.seen[t] = name
	props := map[string]any{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		jn := strings.Split(tag, ",")[0]
		if f.Anonymous && jn == "" {
			// embedded: flatten
			if f.Type.Kind() == reflect.Struct {
				sub := g.schemaOf(f.Type.Name(), f.Type)
				if ref, ok := sub["$ref"]; ok {
					if def, ok := g.schemas[strings.TrimPrefix(ref.(string), "#/components/schemas/")].(map[string]any); ok {
						for k, v := range def["properties"].(map[string]any) {
							props[k] = v
						}
					}
				}
			}
			continue
		}
		if jn == "" {
			jn = f.Name
		}
		props[jn] = g.typeSchema(f.Type)
		if f.Type.Kind() != reflect.Ptr && f.Type.Kind() != reflect.Slice && f.Type.Kind() != reflect.Map && !strings.Contains(tag, "omitempty") {
			required = append(required, jn)
		}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	g.schemas[name] = s
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

var (
	uuidT = reflect.TypeOf(uuid.UUID{})
	timeT = reflect.TypeOf(time.Time{})
)

func (g *gen) typeSchema(t reflect.Type) map[string]any {
	switch {
	case t == uuidT:
		return map[string]any{"type": "string", "format": "uuid"}
	case t == timeT:
		return map[string]any{"type": "string", "format": "date-time"}
	}
	switch t.Kind() {
	case reflect.Ptr:
		s := g.typeSchema(t.Elem())
		s["nullable"] = true
		return s
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "object", "description": "JSON"}
		}
		return map[string]any{"type": "array", "items": g.typeSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": true}
	case reflect.Struct:
		name := t.Name()
		if name == "" {
			name = "Anon" + fmt.Sprint(len(g.schemas))
		}
		return g.schemaOf(name, t)
	case reflect.Interface:
		return map[string]any{}
	}
	return map[string]any{"type": "string"}
}

func tagOf(route string) string {
	r := strings.TrimPrefix(route, "/api/v1/")
	seg := strings.Split(r, "/")[0]
	switch seg {
	case "auth", "me":
		return "Auth"
	case "users", "roles", "permissions", "teams":
		return "IAM"
	case "organizations", "locations", "properties", "buildings", "towers", "floors", "areas", "spaces", "units", "tenants", "occupants":
		return "Property"
	case "tasks", "work-orders", "checklist-templates", "checklist-runs", "checklist-run-items", "sla-policies", "links", "activities", "comments":
		return "Operations"
	case "incidents", "findings":
		return "Operations"
	case "assets", "equipment", "qr":
		return "Asset Management"
	case "maintenance-plans", "maintenance-schedules", "inspections":
		return "Engineering"
	case "checkpoints", "patrol-routes", "patrol-schedules", "patrol-tasks":
		return "Security"
	case "cleaning-schedules", "cleaning-tasks", "housekeeping-inspections":
		return "Housekeeping"
	case "service-requests", "service-request-categories":
		return "Tenant"
	case "notifications":
		return "Notification"
	case "overview":
		return "Overview"
	case "search":
		return "Search"
	case "exports":
		return "Export"
	case "sync":
		return "Sync"
	case "attachments":
		return "Attachments"
	case "audit-logs":
		return "Audit"
	}
	if strings.HasPrefix(route, "/public") {
		return "Public Intake"
	}
	return "System"
}

func tags() []map[string]any {
	var out []map[string]any
	for _, t := range []string{"System", "Auth", "IAM", "Property", "Operations", "Asset Management", "Engineering", "Security", "Housekeeping", "Tenant", "Notification", "Overview", "Search", "Export", "Sync", "Attachments", "Audit", "Public Intake"} {
		out = append(out, map[string]any{"name": t})
	}
	return out
}

func summaryOf(method, route string) string {
	r := strings.TrimPrefix(route, "/api/v1")
	segs := strings.Split(strings.Trim(r, "/"), "/")
	last := segs[len(segs)-1]
	switch {
	case strings.HasPrefix(last, "{"):
		if method == http.MethodGet {
			return "Get " + singular(segs[len(segs)-2])
		}
		if method == http.MethodPatch {
			return "Update " + singular(segs[len(segs)-2])
		}
		if method == http.MethodDelete {
			return "Delete " + singular(segs[len(segs)-2])
		}
	case method == http.MethodPost && len(segs) >= 3 && strings.HasPrefix(segs[len(segs)-2], "{"):
		return strings.Title(strings.ReplaceAll(last, "-", " ")) + " " + singular(segs[len(segs)-3])
	case method == http.MethodGet:
		return "List " + strings.ReplaceAll(last, "-", " ")
	case method == http.MethodPost:
		return "Create " + singular(last)
	}
	return method + " " + r
}

func singular(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	if strings.HasSuffix(s, "ies") {
		return strings.TrimSuffix(s, "ies") + "y"
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") {
		return strings.TrimSuffix(s, "s")
	}
	return s
}

func bodyFor(method, route string) string {
	if method != http.MethodPost && method != http.MethodPatch && method != http.MethodPut {
		return ""
	}
	r := strings.TrimPrefix(route, "/api/v1")
	switch {
	case r == "/tasks":
		return "CreateTaskInput"
	case r == "/work-orders":
		return "CreateWorkOrderInput"
	case strings.HasSuffix(r, "/assign"):
		return "AssignInput"
	case strings.HasPrefix(r, "/tasks/{id}/") || strings.HasPrefix(r, "/work-orders/{id}/"):
		if strings.HasSuffix(r, "/comments") {
			return "Anon_Comment"
		}
		if strings.HasSuffix(r, "/links") {
			return "LinkRef"
		}
		if strings.HasSuffix(r, "/checklist-runs") {
			return ""
		}
		return "TransitionInput"
	case method == http.MethodPatch && (strings.HasPrefix(r, "/tasks/") || strings.HasPrefix(r, "/work-orders/")):
		return "UpdateWorkItemInput"
	case r == "/incidents":
		return "CreateIncidentInput"
	case method == http.MethodPatch && strings.HasPrefix(r, "/incidents/"):
		return "UpdateIncidentInput"
	case r == "/findings":
		return "CreateFindingInput"
	case strings.HasSuffix(r, "/work-orders") && strings.Contains(r, "{id}"):
		return "CreateWorkOrderInput"
	case strings.HasSuffix(r, "/tasks") && strings.Contains(r, "{id}"):
		return "CreateTaskInput"
	case strings.HasSuffix(r, "/incidents") && strings.Contains(r, "{id}"):
		return "CreateIncidentInput"
	case r == "/checklist-templates" || (method == http.MethodPatch && strings.HasPrefix(r, "/checklist-templates/")):
		return "ChecklistTemplateInput"
	case strings.HasSuffix(r, "/answer"):
		return "AnswerInput"
	case r == "/sla-policies":
		return "SLAPolicyInput"
	case r == "/service-requests":
		return "CreateServiceRequestInput"
	case method == http.MethodPatch && strings.HasPrefix(r, "/service-requests/"):
		return "UpdateServiceRequestInput"
	case r == "/assets" || (method == http.MethodPatch && strings.HasPrefix(r, "/assets/")):
		return "AssetInput"
	case r == "/equipment" || (method == http.MethodPatch && strings.HasPrefix(r, "/equipment/")):
		return "EquipmentInput"
	case r == "/maintenance-plans" || (method == http.MethodPatch && strings.HasPrefix(r, "/maintenance-plans/")):
		return "MaintenancePlanInput"
	case r == "/inspections":
		return "CreateInspectionInput"
	case r == "/checkpoints" || (method == http.MethodPatch && strings.HasPrefix(r, "/checkpoints/")):
		return "CheckpointInput"
	case r == "/patrol-routes" || (method == http.MethodPatch && strings.HasPrefix(r, "/patrol-routes/")):
		return "PatrolRouteInput"
	case r == "/patrol-schedules" || (method == http.MethodPatch && strings.HasPrefix(r, "/patrol-schedules/")):
		return "PatrolScheduleInput"
	case strings.HasSuffix(r, "/scans"):
		return "ScanInput"
	case r == "/cleaning-schedules" || (method == http.MethodPatch && strings.HasPrefix(r, "/cleaning-schedules/")):
		return "CleaningScheduleInput"
	case r == "/housekeeping-inspections":
		return "CreateHousekeepingInspectionInput"
	case r == "/locations" || r == "/properties" || r == "/buildings" || r == "/towers" || r == "/floors" || r == "/areas" || r == "/spaces" || r == "/units":
		return "CreateLocationInput"
	case method == http.MethodPatch && (strings.HasPrefix(r, "/locations/") || strings.HasPrefix(r, "/properties/") || strings.HasPrefix(r, "/buildings/") || strings.HasPrefix(r, "/floors/") || strings.HasPrefix(r, "/units/") || strings.HasPrefix(r, "/areas/") || strings.HasPrefix(r, "/towers/") || strings.HasPrefix(r, "/spaces/")):
		return "UpdateLocationInput"
	case r == "/tenants" || (method == http.MethodPatch && strings.HasPrefix(r, "/tenants/")):
		return "TenantInput"
	case r == "/users":
		return "CreateUserInput"
	case method == http.MethodPatch && strings.HasPrefix(r, "/users/"):
		return "UpdateUserInput"
	case r == "/roles" || (method == http.MethodPatch && strings.HasPrefix(r, "/roles/")):
		return "RoleInput"
	case r == "/teams" || (method == http.MethodPatch && strings.HasPrefix(r, "/teams/")):
		return "TeamInput"
	case r == "/me/devices":
		return "DeviceInput"
	case r == "/attachments/presign":
		return "PresignInput"
	case strings.HasSuffix(r, "/confirm"):
		return "ConfirmInput"
	case r == "/sync/mutations":
		return "SyncPushInput"
	case r == "/notifications/preferences":
		return "NotificationPreference"
	}
	return ""
}

func responsesFor(method, route string) map[string]any {
	r := strings.TrimPrefix(route, "/api/v1")
	ok := map[string]any{"description": "OK"}
	schema := ""
	switch {
	case strings.HasPrefix(r, "/tasks") || strings.HasPrefix(r, "/work-orders") || strings.HasPrefix(r, "/patrol-tasks") || strings.HasPrefix(r, "/cleaning-tasks") || r == "/inspections" || r == "/housekeeping-inspections":
		schema = "WorkItem"
	case strings.HasPrefix(r, "/service-requests"):
		schema = "ServiceRequest"
	case strings.HasPrefix(r, "/incidents"):
		schema = "Incident"
	case strings.HasPrefix(r, "/findings"):
		schema = "Finding"
	case strings.HasPrefix(r, "/assets"):
		schema = "Asset"
	case strings.HasPrefix(r, "/equipment"):
		schema = "Equipment"
	case strings.HasPrefix(r, "/maintenance-plans"):
		schema = "MaintenancePlan"
	case strings.HasPrefix(r, "/maintenance-schedules"):
		schema = "MaintenanceSchedule"
	case strings.HasPrefix(r, "/checkpoints"):
		schema = "Checkpoint"
	case strings.HasPrefix(r, "/patrol-routes"):
		schema = "PatrolRoute"
	case strings.HasPrefix(r, "/patrol-schedules"):
		schema = "PatrolSchedule"
	case strings.HasPrefix(r, "/cleaning-schedules"):
		schema = "CleaningSchedule"
	case strings.HasPrefix(r, "/locations") || strings.HasPrefix(r, "/properties") || strings.HasPrefix(r, "/buildings") || strings.HasPrefix(r, "/floors") || strings.HasPrefix(r, "/units") || strings.HasPrefix(r, "/areas") || strings.HasPrefix(r, "/towers") || strings.HasPrefix(r, "/spaces"):
		schema = "Location"
	case strings.HasPrefix(r, "/tenants"):
		schema = "Tenant"
	case strings.HasPrefix(r, "/users"):
		schema = "User"
	case strings.HasPrefix(r, "/roles"):
		schema = "Role"
	case strings.HasPrefix(r, "/teams"):
		schema = "Team"
	case strings.HasPrefix(r, "/attachments"):
		schema = "Attachment"
	case strings.HasPrefix(r, "/notifications"):
		schema = "Notification"
	case strings.HasPrefix(r, "/search"):
		schema = "SearchResult"
	case strings.HasPrefix(r, "/exports"):
		schema = "Export"
	case strings.HasPrefix(r, "/sync/work-bundle"):
		schema = "SyncBundle"
	case strings.HasPrefix(r, "/sync/mutations"):
		schema = "SyncPushOutput"
	case strings.HasPrefix(r, "/sync/conflicts"):
		schema = "SyncConflict"
	case strings.HasPrefix(r, "/audit-logs"):
		schema = "AuditLog"
	case strings.HasPrefix(r, "/auth"):
		schema = "TokenPair"
	case r == "/overview/today":
		schema = "OverviewToday"
	case r == "/overview/attention-required":
		schema = "AttentionItem"
	case r == "/overview/todays-operations":
		schema = "TodaysOperations"
	case r == "/overview/team-workload":
		schema = "WorkloadRow"
	case r == "/overview/building-state":
		schema = "BuildingState"
	case r == "/overview/tenant-requests":
		schema = "TenantRequestsPanel"
	case r == "/overview/pm-due":
		schema = "MaintenanceSchedule"
	}
	if schema != "" {
		ref := map[string]any{"$ref": "#/components/schemas/" + schema}
		if method == http.MethodGet && !strings.Contains(r, "{") && r != "/overview/today" && r != "/overview/tenant-requests" && r != "/sync/work-bundle" {
			ref = map[string]any{"type": "object", "properties": map[string]any{"data": map[string]any{"type": "array", "items": ref}, "next_cursor": map[string]any{"type": "string", "nullable": true}}}
		}
		ok["content"] = map[string]any{"application/json": map[string]any{"schema": ref}}
	}
	code := "200"
	if method == http.MethodPost && !strings.Contains(r, "{") {
		code = "201"
	}
	return map[string]any{
		code:  ok,
		"400": map[string]any{"description": "Validation error", "content": map[string]any{"application/problem+json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Problem"}}}},
		"401": map[string]any{"description": "Unauthorized"},
		"403": map[string]any{"description": "Forbidden"},
		"404": map[string]any{"description": "Not found"},
		"409": map[string]any{"description": "Conflict / invalid transition"},
	}
}

func init() { _ = os.Stdout }
