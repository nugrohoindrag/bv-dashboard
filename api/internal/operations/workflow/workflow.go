// Package workflow: state machine data-driven untuk Task, Work Order, Incident, Finding, Service Request
// (PRD §9.1, §16.3; Naming Convention §25, §27; TAD §5.8; TD-001: status awal `new`).
// Tidak import DB/HTTP.
package workflow

import (
	"fmt"
)

type Status string

const (
	New              Status = "new"
	Scheduled        Status = "scheduled"
	Assigned         Status = "assigned"
	InProgress       Status = "in_progress"
	OnHold           Status = "on_hold"
	Completed        Status = "completed"
	Closed           Status = "closed"
	Cancelled        Status = "cancelled"
	Acknowledged     Status = "acknowledged"
	WaitingForTenant Status = "waiting_for_tenant"
	Resolved         Status = "resolved"
	Open             Status = "open"
)

// Action names (verb; sub-resource POST /{object}/{id}/{action})
const (
	ActSchedule    = "schedule"
	ActAssign      = "assign"
	ActStart       = "start"
	ActHold        = "hold"
	ActResume      = "resume"
	ActComplete    = "complete"
	ActClose       = "close"
	ActReopen      = "reopen"
	ActCancel      = "cancel"
	ActAcknowledge = "acknowledge"
	ActResolve     = "resolve"
	ActWaitTenant  = "wait_tenant"
	ActUnassign    = "unassign"
)

// GuardInput adalah fakta yang dibutuhkan guard; dihitung oleh service, bukan oleh package ini.
type GuardInput struct {
	IsAssignee        bool // user = assignee, atau anggota team assignee
	HasManage         bool // pemegang permission *.manage (boleh on-behalf)
	EvidenceSatisfied bool
	EvidenceReason    string // alasan bila belum satisfied (untuk pesan error)
	HasAssignee       bool
}

type Guard func(in GuardInput) error

func IsAssignee(in GuardInput) error {
	if in.IsAssignee || in.HasManage {
		return nil
	}
	return fmt.Errorf("hanya assignee (atau pemegang izin manage) yang dapat melakukan aksi ini")
}

func EvidenceSatisfied(in GuardInput) error {
	if in.EvidenceSatisfied {
		return nil
	}
	if in.EvidenceReason != "" {
		return fmt.Errorf("%s", in.EvidenceReason)
	}
	return fmt.Errorf("evidence belum lengkap")
}

func RequiresAssignee(in GuardInput) error {
	if in.HasAssignee {
		return nil
	}
	return fmt.Errorf("object belum memiliki assignee")
}

func And(gs ...Guard) Guard {
	return func(in GuardInput) error {
		for _, g := range gs {
			if err := g(in); err != nil {
				return err
			}
		}
		return nil
	}
}

type Transition struct {
	From          []Status
	To            Status
	Action        string
	Perm          string // permission yang wajib (kosong = hanya guard)
	Guard         Guard
	RequireReason bool
	// AllowPermBypassGuard: bila true, pemegang Perm boleh bypass guard (mis. supervisor complete on-behalf).
	AllowPermBypassGuard bool
}

type Workflow struct {
	ObjectType  string
	Initial     Status
	Transitions []Transition
	Terminal    []Status
}

func Any(s ...Status) []Status { return s }

// Find mencari transisi untuk (from, action).
func (w Workflow) Find(from Status, action string) (*Transition, bool) {
	for i := range w.Transitions {
		t := &w.Transitions[i]
		if t.Action != action {
			continue
		}
		for _, f := range t.From {
			if f == from {
				return t, true
			}
		}
	}
	return nil, false
}

func (w Workflow) IsTerminal(s Status) bool {
	for _, t := range w.Terminal {
		if t == s {
			return true
		}
	}
	return false
}

// ActionsFrom: daftar aksi yang mungkin dari status tertentu (sebelum cek permission/guard).
func (w Workflow) ActionsFrom(from Status) []Transition {
	var out []Transition
	for _, t := range w.Transitions {
		for _, f := range t.From {
			if f == from {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// ValidStatus memeriksa apakah status dikenal workflow.
func (w Workflow) ValidStatus(s Status) bool {
	if s == w.Initial {
		return true
	}
	for _, t := range w.Transitions {
		if t.To == s {
			return true
		}
		for _, f := range t.From {
			if f == s {
				return true
			}
		}
	}
	return false
}

// ---------- Definisi ----------

func workItemWorkflow(objectType, permObj string) Workflow {
	p := func(a string) string { return "operations." + permObj + "." + a }
	return Workflow{
		ObjectType: objectType,
		Initial:    New,
		Terminal:   []Status{Closed, Cancelled},
		Transitions: []Transition{
			{From: Any(New), To: Scheduled, Action: ActSchedule, Perm: p("update")},
			{From: Any(New, Scheduled, Assigned), To: Assigned, Action: ActAssign, Perm: p("assign")},
			{From: Any(Assigned), To: New, Action: ActUnassign, Perm: p("assign")},
			{From: Any(Assigned, Scheduled), To: InProgress, Action: ActStart, Perm: p("start"), Guard: And(RequiresAssignee, IsAssignee)},
			{From: Any(InProgress), To: OnHold, Action: ActHold, Perm: p("start"), Guard: IsAssignee, RequireReason: true},
			{From: Any(OnHold), To: InProgress, Action: ActResume, Perm: p("start"), Guard: IsAssignee},
			{From: Any(InProgress), To: Completed, Action: ActComplete, Perm: p("complete"), Guard: And(IsAssignee, EvidenceSatisfied)},
			{From: Any(Completed), To: Closed, Action: ActClose, Perm: p("close")},
			{From: Any(Completed, Closed), To: InProgress, Action: ActReopen, Perm: p(reopenPerm(permObj)), RequireReason: true},
			{From: Any(New, Scheduled, Assigned, InProgress, OnHold), To: Cancelled, Action: ActCancel, Perm: p("cancel"), RequireReason: true},
		},
	}
}

func reopenPerm(permObj string) string {
	if permObj == "work_orders" {
		return "reopen" // FR-WO-014
	}
	return "close"
}

var Task = workItemWorkflow("task", "tasks")
var WorkOrder = workItemWorkflow("work_order", "work_orders")

var Incident = Workflow{
	ObjectType: "incident",
	Initial:    New,
	Terminal:   []Status{Closed, Cancelled},
	Transitions: []Transition{
		{From: Any(New, Assigned), To: Assigned, Action: ActAssign, Perm: "operations.incidents.assign"},
		{From: Any(New, Assigned), To: InProgress, Action: ActStart, Perm: "operations.incidents.update", Guard: IsAssignee, AllowPermBypassGuard: true},
		{From: Any(New, Assigned, InProgress), To: Resolved, Action: ActResolve, Perm: "operations.incidents.resolve", RequireReason: true},
		{From: Any(Resolved), To: Closed, Action: ActClose, Perm: "operations.incidents.close"},
		{From: Any(Resolved, Closed), To: InProgress, Action: ActReopen, Perm: "operations.incidents.close", RequireReason: true},
		{From: Any(New, Assigned, InProgress), To: Cancelled, Action: ActCancel, Perm: "operations.incidents.manage", RequireReason: true},
	},
}

var Finding = Workflow{
	ObjectType: "finding",
	Initial:    Open,
	Terminal:   []Status{Closed},
	Transitions: []Transition{
		{From: Any(Open), To: InProgress, Action: ActStart, Perm: "operations.findings.update"},
		{From: Any(Open, InProgress), To: Resolved, Action: ActResolve, Perm: "operations.findings.resolve", RequireReason: true},
		{From: Any(Resolved), To: Closed, Action: ActClose, Perm: "operations.findings.close"},
		{From: Any(Resolved, Closed), To: InProgress, Action: ActReopen, Perm: "operations.findings.close", RequireReason: true},
	},
}

// ServiceRequest (Naming Convention §27; PRD §16.3)
var ServiceRequest = Workflow{
	ObjectType: "service_request",
	Initial:    New,
	Terminal:   []Status{Closed, Cancelled},
	Transitions: []Transition{
		{From: Any(New), To: Acknowledged, Action: ActAcknowledge, Perm: "tenant.service_requests.acknowledge"},
		{From: Any(New, Acknowledged, Assigned), To: Assigned, Action: ActAssign, Perm: "tenant.service_requests.assign"},
		{From: Any(Acknowledged, Assigned, WaitingForTenant), To: InProgress, Action: ActStart, Perm: "tenant.service_requests.update"},
		{From: Any(InProgress, Assigned), To: WaitingForTenant, Action: ActWaitTenant, Perm: "tenant.service_requests.update", RequireReason: true},
		{From: Any(Acknowledged, Assigned, InProgress, WaitingForTenant), To: Resolved, Action: ActResolve, Perm: "tenant.service_requests.resolve", RequireReason: true},
		{From: Any(Resolved), To: Closed, Action: ActClose, Perm: "tenant.service_requests.close"},
		{From: Any(Resolved, Closed), To: InProgress, Action: ActReopen, Perm: "tenant.service_requests.close", RequireReason: true},
		{From: Any(New, Acknowledged, Assigned, InProgress, WaitingForTenant), To: Cancelled, Action: ActCancel, Perm: "tenant.service_requests.cancel", RequireReason: true},
	},
}

// ByObjectType mengembalikan workflow untuk object type.
func ByObjectType(ot string) (Workflow, bool) {
	switch ot {
	case "task":
		return Task, true
	case "work_order":
		return WorkOrder, true
	case "incident":
		return Incident, true
	case "finding":
		return Finding, true
	case "service_request":
		return ServiceRequest, true
	}
	return Workflow{}, false
}

// Label untuk pesan (Naming Convention §25).
func Label(s Status) string {
	switch s {
	case New:
		return "New"
	case Scheduled:
		return "Scheduled"
	case Assigned:
		return "Assigned"
	case InProgress:
		return "In Progress"
	case OnHold:
		return "On Hold"
	case Completed:
		return "Completed"
	case Closed:
		return "Closed"
	case Cancelled:
		return "Cancelled"
	case Acknowledged:
		return "Acknowledged"
	case WaitingForTenant:
		return "Waiting for Tenant"
	case Resolved:
		return "Resolved"
	case Open:
		return "Open"
	}
	return string(s)
}
