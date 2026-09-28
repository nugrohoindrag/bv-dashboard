package billing

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

// upload_proof hanya untuk tenant pemilik pembayaran manual yang menunggu; kwitansi untuk yang lunas.
func TestTenantPaymentActionsOwnerOnly(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	pending := func() *Payment {
		return &Payment{Status: "pending", ProviderCode: "manual", TenantUserID: &owner}
	}
	v := pending()
	tenantPaymentActions(v, owner)
	if !slices.Contains(v.AllowedActions, "upload_proof") {
		t.Fatalf("pemilik harus bisa unggah bukti: %v", v.AllowedActions)
	}
	v = pending()
	tenantPaymentActions(v, other)
	if slices.Contains(v.AllowedActions, "upload_proof") {
		t.Fatalf("anggota lain tidak boleh unggah bukti: %v", v.AllowedActions)
	}
	v = &Payment{Status: "pending", ProviderCode: "manual"} // dibuat staf (tanpa tenant_user_id)
	tenantPaymentActions(v, owner)
	if slices.Contains(v.AllowedActions, "upload_proof") {
		t.Fatalf("pembayaran tanpa pemilik tenant: %v", v.AllowedActions)
	}
	v = &Payment{Status: "paid", ProviderCode: "manual", TenantUserID: &owner}
	tenantPaymentActions(v, other)
	if !slices.Contains(v.AllowedActions, "download_receipt") || slices.Contains(v.AllowedActions, "upload_proof") {
		t.Fatalf("pembayaran lunas: %v", v.AllowedActions)
	}
	v = &Payment{Status: "refunded", ProviderCode: "manual", TenantUserID: &owner}
	tenantPaymentActions(v, owner)
	if !slices.Contains(v.AllowedActions, "download_receipt") {
		t.Fatalf("kwitansi pembayaran refund tetap tersedia: %v", v.AllowedActions)
	}
}
