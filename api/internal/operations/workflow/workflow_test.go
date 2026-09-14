package workflow

import (
	"testing"
)

// Exit criteria P0.2 (TAD §15): state machine unit-test 100% transisi.
func TestWorkItemLifecycle(t *testing.T) {
	for _, wf := range []Workflow{Task, WorkOrder} {
		t.Run(wf.ObjectType, func(t *testing.T) {
			if wf.Initial != New {
				t.Fatalf("TD-001: status awal harus new, got %s", wf.Initial)
			}
			// jalur utama PRD §9.1: new → scheduled → assigned → in_progress → completed → closed
			path := []struct {
				from   Status
				action string
				to     Status
			}{
				{New, ActSchedule, Scheduled},
				{Scheduled, ActAssign, Assigned},
				{Assigned, ActStart, InProgress},
				{InProgress, ActHold, OnHold},
				{OnHold, ActResume, InProgress},
				{InProgress, ActComplete, Completed},
				{Completed, ActClose, Closed},
			}
			for _, p := range path {
				tr, ok := wf.Find(p.from, p.action)
				if !ok {
					t.Fatalf("transisi %s --%s--> tidak ada", p.from, p.action)
				}
				if tr.To != p.to {
					t.Fatalf("%s --%s--> %s, expected %s", p.from, p.action, tr.To, p.to)
				}
			}
			// Completed ≠ Closed: close hanya dari completed
			for _, from := range []Status{New, Scheduled, Assigned, InProgress, OnHold} {
				if _, ok := wf.Find(from, ActClose); ok {
					t.Errorf("close tidak boleh dari %s", from)
				}
			}
			// complete hanya dari in_progress
			for _, from := range []Status{New, Scheduled, Assigned, OnHold, Completed, Closed} {
				if _, ok := wf.Find(from, ActComplete); ok {
					t.Errorf("complete tidak boleh dari %s", from)
				}
			}
			// cancel tidak dari completed/closed
			for _, from := range []Status{Completed, Closed, Cancelled} {
				if _, ok := wf.Find(from, ActCancel); ok {
					t.Errorf("cancel tidak boleh dari %s", from)
				}
			}
			// reopen wajib alasan
			tr, _ := wf.Find(Closed, ActReopen)
			if !tr.RequireReason {
				t.Error("reopen harus RequireReason")
			}
			// terminal
			if !wf.IsTerminal(Closed) || !wf.IsTerminal(Cancelled) || wf.IsTerminal(Completed) {
				t.Error("terminal states salah")
			}
			// guard complete: assignee + evidence
			tr, _ = wf.Find(InProgress, ActComplete)
			if err := tr.Guard(GuardInput{IsAssignee: true, EvidenceSatisfied: false, EvidenceReason: "After Photo wajib"}); err == nil {
				t.Error("complete tanpa evidence harus gagal")
			}
			if err := tr.Guard(GuardInput{IsAssignee: false, EvidenceSatisfied: true}); err == nil {
				t.Error("complete oleh bukan assignee harus gagal")
			}
			if err := tr.Guard(GuardInput{IsAssignee: false, HasManage: true, EvidenceSatisfied: true}); err != nil {
				t.Errorf("manage boleh on-behalf: %v", err)
			}
			if err := tr.Guard(GuardInput{IsAssignee: true, EvidenceSatisfied: true}); err != nil {
				t.Errorf("complete valid: %v", err)
			}
			// start tanpa assignee
			tr, _ = wf.Find(Assigned, ActStart)
			if err := tr.Guard(GuardInput{HasAssignee: false, IsAssignee: true}); err == nil {
				t.Error("start tanpa assignee harus gagal")
			}
		})
	}
	// WO reopen memakai permission reopen (FR-WO-014), Task memakai close
	tr, _ := WorkOrder.Find(Completed, ActReopen)
	if tr.Perm != "operations.work_orders.reopen" {
		t.Errorf("WO reopen perm: %s", tr.Perm)
	}
	tr, _ = Task.Find(Completed, ActReopen)
	if tr.Perm != "operations.tasks.close" {
		t.Errorf("Task reopen perm: %s", tr.Perm)
	}
}

func TestServiceRequestLifecycle(t *testing.T) {
	path := []struct {
		from   Status
		action string
		to     Status
	}{
		{New, ActAcknowledge, Acknowledged},
		{Acknowledged, ActAssign, Assigned},
		{Assigned, ActStart, InProgress},
		{InProgress, ActWaitTenant, WaitingForTenant},
		{WaitingForTenant, ActStart, InProgress},
		{InProgress, ActResolve, Resolved},
		{Resolved, ActClose, Closed},
	}
	for _, p := range path {
		tr, ok := ServiceRequest.Find(p.from, p.action)
		if !ok || tr.To != p.to {
			t.Fatalf("SR %s --%s--> expected %s", p.from, p.action, p.to)
		}
	}
	if _, ok := ServiceRequest.Find(New, ActClose); ok {
		t.Error("SR close dari new tidak boleh")
	}
}

func TestIncidentAndFinding(t *testing.T) {
	for _, s := range []struct {
		wf     Workflow
		from   Status
		action string
		to     Status
	}{
		{Incident, New, ActAssign, Assigned},
		{Incident, Assigned, ActStart, InProgress},
		{Incident, InProgress, ActResolve, Resolved},
		{Incident, Resolved, ActClose, Closed},
		{Finding, Open, ActStart, InProgress},
		{Finding, InProgress, ActResolve, Resolved},
		{Finding, Open, ActResolve, Resolved},
		{Finding, Resolved, ActClose, Closed},
	} {
		tr, ok := s.wf.Find(s.from, s.action)
		if !ok || tr.To != s.to {
			t.Errorf("%s %s --%s--> expected %s", s.wf.ObjectType, s.from, s.action, s.to)
		}
	}
	if Finding.Initial != Open {
		t.Error("finding initial harus open")
	}
}

func TestAllTransitionsHavePermOrGuard(t *testing.T) {
	for _, wf := range []Workflow{Task, WorkOrder, Incident, Finding, ServiceRequest} {
		for _, tr := range wf.Transitions {
			if tr.Perm == "" && tr.Guard == nil {
				t.Errorf("%s %s: transisi tanpa perm dan guard", wf.ObjectType, tr.Action)
			}
			if !wf.ValidStatus(tr.To) {
				t.Errorf("%s: status %s tidak valid", wf.ObjectType, tr.To)
			}
		}
	}
}
