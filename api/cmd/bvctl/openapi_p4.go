package main

import (
	"net/http"
	"strings"

	"github.com/buildingvision/api/internal/billing"
	"github.com/buildingvision/api/internal/finance"
	"github.com/buildingvision/api/internal/metering"
	"github.com/buildingvision/api/internal/parcels"
	"github.com/buildingvision/api/internal/security"
	"github.com/buildingvision/api/internal/tenantapp"
	"github.com/buildingvision/api/internal/tenantrelation"
	"github.com/buildingvision/api/internal/tenantservice"
	"github.com/buildingvision/api/internal/waassist"
)

// ---------- OpenAPI PRD P3 v2.1 (Tenant Experience) & P4 v2.1 (Financial Operations, bug B-16) ----------
// Skema Billing/Finance/Tenant App eksplisit, tag per modul (sebelumnya jatuh ke "System"), body & respons per route,
// serta parameter query yang benar untuk daftar invoice/pembayaran (bukan parameter generik Operations).

func p3p4Types() map[string]any {
	return map[string]any{
		// Billing
		"Invoice": billing.Invoice{}, "InvoiceInput": billing.InvoiceInput{}, "BillingItem": billing.Item{}, "InvoiceActionInput": billing.ActionInput{},
		"Payment": billing.Payment{}, "RecordPaymentInput": billing.RecordInput{}, "InitiatePaymentInput": billing.InitiateInput{}, "PaymentProvider": billing.ProviderInfo{}, "PaymentProviderInput": billing.ProviderInput{},
		"PaymentUpdateInput": billing.PaymentUpdateInput{}, "PaymentProof": billing.Proof{}, "RefundInput": billing.RefundInput{}, "ApplyBalanceInput": billing.ApplyBalanceInput{},
		"ReceiveInput": billing.ReceiveInput{}, "ReceiveResult": billing.ReceiveResult{}, "PaymentAllocation": billing.Allocation{}, "WebhookResult": billing.WebhookResult{},
		"InvoiceSummary": billing.Summary{}, "CreditNote": billing.CreditNote{}, "CreditNoteInput": billing.CreditNoteInput{}, "CreditNoteDecision": billing.CreditNoteDecision{},
		"BillingSettings": billing.Settings{}, "BillingSettingsInput": billing.SettingsInput{}, "BankAccount": billing.BankAccount{}, "BankAccountInput": billing.BankAccountInput{},
		"BillingRule": billing.Rule{}, "BillingRuleInput": billing.RuleInput{}, "BillingRun": billing.Run{}, "BillingRunInput": billing.RunInput{}, "BillingRunLine": billing.RunLine{},
		"SinkingFundSummary": billing.SinkingFundSummary{}, "FundEntry": billing.FundEntry{}, "FundEntryInput": billing.FundEntryInput{}, "PartyBalance": billing.PartyBalance{},
		"LedgerEntry": billing.LedgerEntry{}, "DepositEntryInput": billing.DepositEntryInput{}, "TenantBalances": billing.TenantBalances{},
		"PenaltyRule": billing.PenaltyRule{}, "PenaltyRuleInput": billing.PenaltyRuleInput{}, "Penalty": billing.Penalty{}, "BillPenaltiesInput": billing.BillPenaltiesInput{}, "BillPenaltiesResult": billing.BillPenaltiesResult{},
		"AgingReport": billing.AgingReport{}, "AgingRow": billing.AgingRow{}, "Statement": billing.Statement{}, "StatementEntry": billing.StatementEntry{},
		"CollectionLog": billing.CollectionLog{}, "CollectionLogInput": billing.CollectionLogInput{}, "CollectionLogUpdate": billing.CollectionLogUpdate{}, "CollectionWorkItem": billing.WorkItem{},
		"BankStatementImport": billing.StatementImport{}, "BankStatementImportInput": billing.ImportInput{}, "BankStatementLine": billing.StmtLine{}, "BankStatementMatchInput": billing.MatchInput{},
		"ChargeDraft": billing.ChargeDraft{}, "ChargeInput": billing.ChargeInput{}, "InvoiceImportInput": billing.InvoiceImportInput{}, "InvoiceImportResult": billing.InvoiceImportResult{},
		"DocumentLink": billing.DocumentLink{},
		// Metering
		"UtilityTariff": metering.Tariff{}, "UtilityTariffInput": metering.TariffInput{}, "Meter": metering.Meter{}, "MeterInput": metering.MeterInput{},
		"MeterReading": metering.Reading{}, "MeterReadingInput": metering.ReadingInput{}, "BundleMeter": metering.BundleMeter{},
		// Finance
		"Budget": finance.Budget{}, "BudgetInput": finance.BudgetInput{}, "BudgetLine": finance.BudgetLine{}, "BudgetVsActual": finance.BVA{}, "BudgetVsActualRow": finance.BVARow{},
		"ActualTransaction": finance.ActualTxn{}, "OperatingCost": finance.OperatingCost{}, "CostEntry": finance.CostEntry{}, "CostEntryInput": finance.CostInput{}, "FinanceCategory": finance.Category{},
		"AccountMapping": finance.Mapping{}, "AccountMappingInput": finance.MappingInput{}, "Journal": finance.Journal{}, "JournalLine": finance.JournalLine{},
		"WebhookEndpoint": finance.Endpoint{}, "WebhookEndpointInput": finance.EndpointInput{}, "WebhookDelivery": finance.Delivery{},
		// PRD P3 v2.1 — Tenant Experience
		"TenantUnitSummary": tenantapp.UnitSummary{}, "TenantAnnouncement": tenantapp.Announcement{}, "TenantFeedbackItem": tenantapp.FeedbackItem{}, "GeneralFeedbackInput": tenantapp.GeneralFeedbackInput{},
		"TenantMember": tenantapp.Member{}, "TenantMemberInput": tenantapp.MemberInput{}, "TenantMemberCreated": tenantapp.MemberCreated{}, "MessageAttachment": tenantapp.MessageAttachment{},
		"Announcement": tenantrelation.Announcement{}, "AnnouncementInput": tenantrelation.AnnouncementInput{}, "AnnouncementBroadcastInput": tenantrelation.BroadcastInput{}, "AnnouncementReads": tenantrelation.AnnouncementReads{},
		"ResetPasswordResult": tenantrelation.ResetPasswordResult{}, "GeneralFeedback": tenantrelation.GeneralFeedback{}, "FeedbackActionInput": tenantrelation.FeedbackActionInput{}, "TenantServiceMetrics": tenantrelation.Metrics{},
		"Package": parcels.Package{}, "PackageInput": parcels.Input{}, "PackageActionInput": parcels.ActionInput{},
		"ParkingPermit": security.ParkingPermit{}, "PermitDecisionInput": security.PermitDecisionInput{}, "TenantVehicle": security.TenantVehicle{}, "TenantVehicleInput": security.TenantVehicleInput{},
		"TenantPermitInput": security.TenantPermitInput{}, "TenantParkingArea": security.TenantParkingArea{},
		"WhatsAppInput": waassist.Input{}, "WhatsAppComposed": waassist.Composed{}, "WhatsAppLog": waassist.LogEntry{},
		"RecurringIssue": tenantservice.RecurringIssue{}, "CommunicationEntry": tenantservice.CommunicationEntry{},
	}
}

// tagP3P4: tag modul untuk route yang sebelumnya "System".
func tagP3P4(route string) string {
	r := strings.TrimPrefix(route, "/api/v1/")
	seg := strings.Split(r, "/")[0]
	switch seg {
	case "invoices", "payments", "payment-providers", "billing", "webhooks":
		return "Billing"
	case "finance":
		return "Finance"
	case "tenant":
		return "Tenant App"
	case "tenant-users", "tenant-relation", "announcements":
		return "Tenant Relation"
	case "packages":
		return "Package"
	case "parking-permits":
		return "Security"
	case "recurring-issues":
		return "Tenant"
	case "whatsapp", "push":
		return "Notification"
	case "bookings":
		return "Facility Booking"
	case "visitors":
		return "Visitor"
	case "inventory", "vendors":
		return "Vendor & Inventory"
	case "hotel":
		return "Hotel"
	case "unit-sales", "unit-rental":
		return "Commercial"
	case "bvrooms":
		return "BVRooms"
	case "admin", "platform", "portfolios", "onboarding", "trial", "growth", "profiles", "client-errors", "staff":
		return "Platform"
	case "public":
		if strings.HasPrefix(r, "public/documents") {
			return "Billing"
		}
		return "Public"
	}
	return ""
}

func tagsP3P4() []string {
	return []string{"Billing", "Finance", "Tenant App", "Tenant Relation", "Package", "Facility Booking", "Visitor", "Vendor & Inventory", "Hotel", "Commercial", "BVRooms", "Platform", "Public"}
}

// bodyP3P4: skema body request (r tanpa prefix /api/v1).
func bodyP3P4(method, r string) string {
	if method != http.MethodPost && method != http.MethodPatch && method != http.MethodPut {
		return ""
	}
	switch {
	case r == "/invoices" || (method == http.MethodPatch && r == "/invoices/{id}"):
		return "InvoiceInput"
	case r == "/invoices/import":
		return "InvoiceImportInput"
	case r == "/invoices/{id}/payments":
		return "RecordPaymentInput"
	case r == "/invoices/{id}/issue" || r == "/invoices/{id}/cancel" || r == "/invoices/{id}/void":
		return "InvoiceActionInput"
	case r == "/invoices/{id}/apply-credit" || r == "/invoices/{id}/apply-deposit":
		return "ApplyBalanceInput"
	case r == "/invoices/{id}/credit-notes":
		return "CreditNoteInput"
	case r == "/payments/receive":
		return "ReceiveInput"
	case r == "/payments/{id}/verify":
		return "RecordPaymentInput"
	case r == "/payments/{id}/refund":
		return "RefundInput"
	case method == http.MethodPatch && r == "/payments/{id}":
		return "PaymentUpdateInput"
	case strings.HasPrefix(r, "/payment-providers/"):
		return "PaymentProviderInput"
	case strings.HasPrefix(r, "/billing/credit-notes/{id}/"):
		return "CreditNoteDecision"
	case r == "/billing/settings":
		return "BillingSettingsInput"
	case strings.HasPrefix(r, "/billing/bank-accounts"):
		return "BankAccountInput"
	case strings.HasPrefix(r, "/billing/rules"):
		return "BillingRuleInput"
	case r == "/billing/runs":
		return "BillingRunInput"
	case r == "/billing/sinking-fund/entries":
		return "FundEntryInput"
	case r == "/billing/deposits/entries":
		return "DepositEntryInput"
	case strings.HasPrefix(r, "/billing/penalty-rules"):
		return "PenaltyRuleInput"
	case r == "/billing/penalties/bill":
		return "BillPenaltiesInput"
	case r == "/billing/collection-logs":
		return "CollectionLogInput"
	case strings.HasPrefix(r, "/billing/collection-logs/"):
		return "CollectionLogUpdate"
	case r == "/billing/bank-statements":
		return "BankStatementImportInput"
	case r == "/billing/bank-statement-lines/{id}/match":
		return "BankStatementMatchInput"
	case r == "/billing/charges":
		return "ChargeInput"
	case strings.HasPrefix(r, "/billing/utility-tariffs"):
		return "UtilityTariffInput"
	case r == "/billing/meters" || (method == http.MethodPatch && r == "/billing/meters/{id}"):
		return "MeterInput"
	case r == "/billing/meters/{id}/readings":
		return "MeterReadingInput"
	case r == "/finance/budgets" || (method == http.MethodPatch && r == "/finance/budgets/{id}"):
		return "BudgetInput"
	case strings.HasPrefix(r, "/finance/costs"):
		return "CostEntryInput"
	case r == "/finance/webhooks" || (method == http.MethodPatch && r == "/finance/webhooks/{id}"):
		return "WebhookEndpointInput"
	case r == "/tenant/invoices/{id}/payments":
		return "InitiatePaymentInput"
	case r == "/tenant/feedback":
		return "GeneralFeedbackInput"
	case r == "/tenant/members":
		return "TenantMemberInput"
	case r == "/tenant/vehicles" || (method == http.MethodPatch && strings.HasPrefix(r, "/tenant/vehicles/")):
		return "TenantVehicleInput"
	case r == "/tenant/parking-permits":
		return "TenantPermitInput"
	case r == "/announcements" || (method == http.MethodPatch && r == "/announcements/{id}"):
		return "AnnouncementInput"
	case r == "/announcements/broadcast":
		return "AnnouncementBroadcastInput"
	case strings.HasPrefix(r, "/tenant-relation/general-feedback/{id}/"):
		return "FeedbackActionInput"
	case r == "/packages" || (method == http.MethodPatch && r == "/packages/{id}"):
		return "PackageInput"
	case strings.HasPrefix(r, "/packages/{id}/"):
		return "PackageActionInput"
	case strings.HasPrefix(r, "/parking-permits/{id}/"):
		return "PermitDecisionInput"
	case r == "/whatsapp/compose" || r == "/whatsapp/send":
		return "WhatsAppInput"
	}
	return ""
}

// responseP3P4: skema respons; list=true → dibungkus {data, next_cursor}.
func responseP3P4(method, r string) (string, bool) {
	get := method == http.MethodGet
	switch {
	case r == "/public/documents/{token}" || strings.HasSuffix(r, "/pdf") || strings.HasSuffix(r, "/receipt") || r == "/finance/journal/export":
		return "", false // file (application/pdf / text/csv / xlsx)
	case strings.HasSuffix(r, "/document-link") || strings.HasSuffix(r, "/statement/link"):
		return "DocumentLink", false
	case r == "/tenant/invoices/summary":
		return "InvoiceSummary", false
	case r == "/invoices/import":
		return "InvoiceImportResult", false
	case r == "/invoices" || strings.HasPrefix(r, "/invoices/{id}") && !strings.HasSuffix(r, "/payments") && !strings.HasSuffix(r, "/credit-notes") && !strings.HasPrefix(r, "/invoices/{id}/apply-"):
		return "Invoice", get && r == "/invoices"
	case r == "/tenant/invoices" || r == "/tenant/invoices/{id}":
		return "Invoice", get && r == "/tenant/invoices"
	case r == "/invoices/{id}/credit-notes" || strings.HasPrefix(r, "/billing/credit-notes"):
		return "CreditNote", get && r == "/billing/credit-notes"
	case r == "/payments/receive":
		return "ReceiveResult", false
	case strings.HasSuffix(r, "/proofs"):
		return "PaymentProof", true
	case r == "/payments" || strings.HasPrefix(r, "/payments/{id}") || r == "/invoices/{id}/payments" || strings.HasPrefix(r, "/invoices/{id}/apply-") || r == "/tenant/payments" || r == "/tenant/payments/{id}" || r == "/tenant/invoices/{id}/payments":
		return "Payment", get && (r == "/payments" || r == "/tenant/payments")
	case strings.HasPrefix(r, "/payment-providers") || r == "/tenant/payment-providers":
		return "PaymentProvider", get
	case r == "/billing/settings":
		return "BillingSettings", false
	case strings.HasPrefix(r, "/billing/bank-accounts"):
		return "BankAccount", get
	case strings.HasPrefix(r, "/billing/rules"):
		return "BillingRule", get && r == "/billing/rules"
	case strings.HasPrefix(r, "/billing/runs"):
		return "BillingRun", get && r == "/billing/runs"
	case r == "/billing/sinking-fund":
		return "SinkingFundSummary", false
	case r == "/billing/sinking-fund/entries":
		return "FundEntry", false
	case r == "/billing/balances":
		return "PartyBalance", true
	case r == "/billing/ledger-entries" || r == "/billing/deposits/entries":
		return "LedgerEntry", get
	case strings.HasPrefix(r, "/billing/penalty-rules"):
		return "PenaltyRule", get
	case r == "/billing/penalties/bill":
		return "BillPenaltiesResult", false
	case strings.HasPrefix(r, "/billing/penalties"):
		return "Penalty", get && r == "/billing/penalties"
	case r == "/billing/aging":
		return "AgingReport", false
	case r == "/billing/statement" || r == "/tenant/statement":
		return "Statement", false
	case r == "/billing/collections/worklist":
		return "CollectionWorkItem", true
	case strings.HasPrefix(r, "/billing/collection-logs"):
		return "CollectionLog", get && r == "/billing/collection-logs"
	case strings.HasPrefix(r, "/billing/bank-statement"):
		return "BankStatementImport", get && r == "/billing/bank-statements"
	case r == "/billing/charges/draft":
		return "ChargeDraft", false
	case r == "/billing/charges":
		return "Invoice", false
	case strings.HasPrefix(r, "/billing/utility-tariffs"):
		return "UtilityTariff", get
	case r == "/billing/meters/{id}/readings" || strings.HasPrefix(r, "/billing/meter-readings"):
		return "MeterReading", get && r == "/billing/meter-readings"
	case strings.HasPrefix(r, "/billing/meters"):
		return "Meter", get && r == "/billing/meters"
	case r == "/tenant/balances":
		return "TenantBalances", false
	case r == "/finance/categories":
		return "FinanceCategory", false
	case strings.HasPrefix(r, "/finance/budgets"):
		return "Budget", get && r == "/finance/budgets"
	case r == "/finance/budget-actual":
		return "BudgetVsActual", false
	case r == "/finance/budget-actual/transactions":
		return "ActualTransaction", true
	case r == "/finance/operating-costs":
		return "OperatingCost", false
	case strings.HasPrefix(r, "/finance/costs"):
		return "CostEntry", get && r == "/finance/costs"
	case r == "/finance/account-mappings":
		return "AccountMapping", true
	case r == "/finance/journal":
		return "Journal", false
	case strings.HasSuffix(r, "/deliveries") || strings.HasPrefix(r, "/finance/webhook-deliveries") || r == "/finance/webhooks/{id}/test":
		return "WebhookDelivery", get
	case strings.HasPrefix(r, "/finance/webhooks"):
		return "WebhookEndpoint", get && r == "/finance/webhooks"
	case r == "/dashboards/finance":
		return "DomainDashboard", false
	case r == "/tenant/units" || r == "/tenant/units/{id}":
		return "TenantUnitSummary", get && r == "/tenant/units"
	case strings.HasPrefix(r, "/tenant/announcements"):
		return "TenantAnnouncement", get && r == "/tenant/announcements"
	case strings.HasPrefix(r, "/tenant/feedback"):
		return "TenantFeedbackItem", get && r == "/tenant/feedback"
	case strings.HasPrefix(r, "/tenant/members"):
		return "TenantMember", get && r == "/tenant/members"
	case strings.HasPrefix(r, "/tenant/vehicles"):
		return "TenantVehicle", get && r == "/tenant/vehicles"
	case strings.HasPrefix(r, "/tenant/parking-permits") || strings.HasPrefix(r, "/parking-permits"):
		return "ParkingPermit", get && (r == "/tenant/parking-permits" || r == "/parking-permits")
	case r == "/tenant/parking-areas":
		return "TenantParkingArea", true
	case strings.HasPrefix(r, "/tenant/packages") || strings.HasPrefix(r, "/packages"):
		return "Package", get && (r == "/tenant/packages" || r == "/packages")
	case strings.HasPrefix(r, "/announcements"):
		if strings.HasSuffix(r, "/reads") {
			return "AnnouncementReads", false
		}
		return "Announcement", get && r == "/announcements"
	case strings.HasPrefix(r, "/tenant-relation/general-feedback"):
		return "GeneralFeedback", get && r == "/tenant-relation/general-feedback"
	case strings.HasSuffix(r, "/reset-password"):
		return "ResetPasswordResult", false
	case strings.HasPrefix(r, "/recurring-issues"):
		return "RecurringIssue", get && r == "/recurring-issues"
	case strings.HasSuffix(r, "/communications"):
		return "CommunicationEntry", true
	case r == "/whatsapp/compose":
		return "WhatsAppComposed", false
	case strings.HasPrefix(r, "/whatsapp"):
		return "WhatsAppLog", get
	}
	return "", false
}

var qp = func(name, typ, desc string) map[string]any {
	s := map[string]any{"type": "string"}
	switch typ {
	case "uuid":
		s["format"] = "uuid"
	case "date":
		s["format"] = "date"
	case "bool":
		s = map[string]any{"type": "boolean"}
	case "int":
		s = map[string]any{"type": "integer"}
	}
	p := map[string]any{"name": name, "in": "query", "schema": s}
	if desc != "" {
		p["description"] = desc
	}
	return p
}

// paramsP3P4: parameter query spesifik (B-16: sebelumnya parameter generik Operations). nil = pakai default generator.
func paramsP3P4(method, r string) []map[string]any {
	if method != http.MethodGet {
		return nil
	}
	page := []map[string]any{qp("limit", "int", "maks. 200"), qp("cursor", "", "")}
	switch r {
	case "/invoices":
		return append(page, qp("property_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("unit_location_id", "uuid", ""), qp("status", "", "CSV: draft,issued,partially_paid,paid,overdue,cancelled"),
			qp("type", "", "invoice_type"), qp("source", "", ""), qp("source_id", "uuid", ""), qp("billing_run_id", "uuid", ""), qp("aging", "", "current|1_30|31_60|61_90|90_plus"),
			qp("due_from", "date", ""), qp("due_to", "date", ""), qp("issued_from", "date", ""), qp("issued_to", "date", ""), qp("open", "bool", ""), qp("overdue", "bool", ""), qp("q", "", ""))
	case "/payments":
		return append(page, qp("property_id", "uuid", ""), qp("invoice_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("status", "", "CSV"), qp("provider", "", ""), qp("method", "", ""),
			qp("receipt_group", "", "RCV-…"), qp("paid_from", "date", ""), qp("paid_to", "date", ""), qp("q", "", ""))
	case "/tenant/invoices":
		return append(page, qp("open", "bool", ""))
	case "/tenant/payments":
		return append(page, qp("invoice_id", "uuid", "B-14: filter server-side"))
	case "/billing/runs", "/billing/credit-notes", "/billing/bank-statements":
		return append(page, qp("property_id", "uuid", ""), qp("status", "", "CSV"), qp("invoice_id", "uuid", ""))
	case "/billing/penalties":
		return append(page, qp("property_id", "uuid", ""), qp("invoice_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("status", "", "CSV"), qp("unbilled", "bool", ""))
	case "/billing/collection-logs":
		return append(page, qp("property_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("unit_location_id", "uuid", ""), qp("invoice_id", "uuid", ""), qp("promise_status", "", "open|kept|broken|cancelled"), qp("follow_up_due", "bool", ""))
	case "/billing/aging":
		return []map[string]any{qp("property_id", "uuid", ""), qp("group_by", "", "tenant|unit|property"), qp("tenant_id", "uuid", "")}
	case "/billing/statement":
		return []map[string]any{qp("property_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("unit_location_id", "uuid", ""), qp("from", "date", ""), qp("to", "date", ""), qp("format", "", "json|pdf")}
	case "/tenant/statement":
		return []map[string]any{qp("from", "date", ""), qp("to", "date", ""), qp("format", "", "json|pdf")}
	case "/billing/balances", "/billing/ledger-entries":
		return []map[string]any{qp("kind", "", "deposit|credit"), qp("property_id", "uuid", ""), qp("tenant_id", "uuid", ""), qp("unit_location_id", "uuid", "")}
	case "/billing/sinking-fund":
		return []map[string]any{qp("property_id", "uuid", ""), qp("from", "date", ""), qp("to", "date", "")}
	case "/billing/charges/draft":
		return []map[string]any{qp("source_type", "", "work_order|service_request"), qp("source_id", "uuid", "")}
	case "/billing/meters":
		return append(page, qp("property_id", "uuid", ""), qp("location_id", "uuid", ""), qp("meter_type", "", "electricity|water"), qp("status", "", ""), qp("unread", "bool", "belum dicatat periode ini"), qp("q", "", ""))
	case "/billing/meter-readings":
		return append(page, qp("property_id", "uuid", ""), qp("meter_id", "uuid", ""), qp("period", "", "YYYY-MM"), qp("status", "", "CSV"), qp("meter_type", "", ""))
	case "/finance/budgets":
		return []map[string]any{qp("property_id", "uuid", ""), qp("year", "int", "")}
	case "/finance/budget-actual", "/finance/operating-costs":
		return []map[string]any{qp("property_id", "uuid", ""), qp("year", "int", "")}
	case "/finance/budget-actual/transactions":
		return []map[string]any{qp("property_id", "uuid", ""), qp("kind", "", "revenue|cost"), qp("category", "", ""), qp("year", "int", ""), qp("month", "int", "0 = setahun")}
	case "/finance/costs":
		return append(page, qp("property_id", "uuid", ""), qp("category", "", ""), qp("from", "date", ""), qp("to", "date", ""), qp("q", "", ""))
	case "/finance/journal", "/finance/journal/export":
		return []map[string]any{qp("property_id", "uuid", ""), qp("from", "date", ""), qp("to", "date", ""), qp("format", "", "csv|xlsx (export)")}
	case "/dashboards/finance":
		return []map[string]any{qp("property_id", "uuid", ""), qp("location_id", "uuid", ""), qp("from", "date", ""), qp("to", "date", "")}
	}
	return nil
}
