package billing

import (
	"testing"

	"github.com/google/uuid"
)

// Edit draft invoice billing run/denda: item lama yang dikirim ulang tanpa jumlah & tautan tetap membawa tautan sumber, dan jumlah
// tepercaya (prorata/tarif meter) dipertahankan selama kuantitas & harga tidak berubah.
func TestMergeItemsKeepsSourceLinks(t *testing.T) {
	rule, meter, penalty := uuid.New(), uuid.New(), uuid.New()
	ct, src := "utility", "invoice_penalty"
	old := []Item{
		{ID: uuid.New(), Description: "Listrik", Quantity: 120, UnitPrice: 1500, Amount: 193500, ChargeType: &ct, BillingRuleID: &rule, MeterReadingID: &meter, Meta: map[string]any{"start": 100.0, "end": 220.0}},
		{ID: uuid.New(), Description: "Denda", Quantity: 1, UnitPrice: 5000, Amount: 5000, SourceType: &src, SourceID: &penalty},
	}
	in := []Item{
		{ID: old[0].ID, Description: "Listrik September", Quantity: 120, UnitPrice: 1500},
		{ID: old[1].ID, Description: "Denda", Quantity: 2, UnitPrice: 5000},
		{Description: "Biaya administrasi", Quantity: 1, UnitPrice: 10000},
	}
	got := mergeItems(old, in)
	if got[0].Amount != 193500 || got[0].BillingRuleID == nil || *got[0].BillingRuleID != rule || got[0].MeterReadingID == nil || *got[0].MeterReadingID != meter {
		t.Fatalf("item meter harus mewarisi jumlah & tautan: %+v", got[0])
	}
	if got[0].Meta == nil || got[0].ChargeType == nil || *got[0].ChargeType != "utility" || got[0].Description != "Listrik September" {
		t.Fatalf("item meter: meta/charge_type diwarisi, deskripsi baru dipakai: %+v", got[0])
	}
	if got[1].Amount != 0 || got[1].SourceType == nil || *got[1].SourceType != src || *got[1].SourceID != penalty {
		t.Fatalf("kuantitas berubah → jumlah dihitung ulang, tautan tetap: %+v", got[1])
	}
	if got[2].BillingRuleID != nil || got[2].SourceType != nil || got[2].Amount != 0 {
		t.Fatalf("item baru tidak mewarisi apa pun: %+v", got[2])
	}
	// tautan yang dikirim klien tidak ditimpa
	other := uuid.New()
	got = mergeItems(old, []Item{{ID: old[0].ID, Description: "Listrik", Quantity: 120, UnitPrice: 1500, Amount: 180000, BillingRuleID: &other}})
	if *got[0].BillingRuleID != other || got[0].Amount != 180000 {
		t.Fatalf("nilai kiriman klien dipakai: %+v", got[0])
	}
}
