// Package ids menghasilkan Business ID (Naming Convention §19–24, §65; PRD §27; TAD §5.9).
// Format: {PREFIX}-{YEAR}-{SEQ6} atau {PREFIX}-{CAT}-{SEQ6} / {PREFIX}-{SEQ6}.
package ids

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/db"
)

const (
	PrefixWorkOrder       = "WO"
	PrefixTask            = "TSK"
	PrefixServiceRequest  = "SR"
	PrefixIncident        = "INC"
	PrefixFinding         = "FND"
	PrefixInspection      = "INS"
	PrefixMaintenancePlan = "PM"
	PrefixAsset           = "AST"
	PrefixChecklist       = "CHK"
	PrefixOrganization    = "ORG"
	PrefixProperty        = "PROP"
	PrefixBuilding        = "BLD"
	PrefixTower           = "TWR"
	PrefixFloor           = "FLR"
	PrefixArea            = "AREA"
	PrefixSpace           = "SPC"
	PrefixUnit            = "UNT"
	PrefixTenant          = "TEN"
	PrefixUser            = "USR"
	// P1 (PRD v1.3 §15 brief; NC v2.0 §35–§36)
	PrefixFacility          = "FCL"   // plain
	PrefixBooking           = "BKG"   // yearly
	PrefixVisitor           = "VIS"   // yearly
	PrefixInvoice           = "INV"   // yearly
	PrefixPayment           = "PAY"   // yearly
	PrefixVendor            = "VND"   // plain
	PrefixItem              = "ITM"   // plain
	PrefixStockTransaction  = "STK"   // yearly
	PrefixHotelReservation  = "RES"   // yearly
	PrefixHotelRoom         = "ROOM"  // yearly
	PrefixHotelRoomType     = "RT"    // yearly
	PrefixHotelRate         = "RATE"  // yearly
	PrefixUnitListing       = "LIST"  // yearly
	PrefixSalesLead         = "LEAD"  // yearly
	PrefixSaleReservation   = "SRES"  // yearly
	PrefixRentalListing     = "RLIST" // yearly
	PrefixRentalReservation = "RRES"  // yearly
)

// nextSeq mengambil nomor urut berikutnya (row lock via upsert) — harus dipanggil dalam transaksi create.
func nextSeq(ctx context.Context, q db.Querier, orgID uuid.UUID, prefix string, year int) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		INSERT INTO business_id_sequences (organization_id, prefix, year, last_value)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (organization_id, prefix, year)
		DO UPDATE SET last_value = business_id_sequences.last_value + 1
		RETURNING last_value`, orgID, prefix, year).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("next business id %s: %w", prefix, err)
	}
	return n, nil
}

// NextYearly: WO-2026-000123. Tahun dihitung dari timezone property (PRD §27).
func NextYearly(ctx context.Context, q db.Querier, orgID uuid.UUID, prefix string, now time.Time, loc *time.Location) (string, error) {
	if loc == nil {
		loc = time.UTC
	}
	year := now.In(loc).Year()
	n, err := nextSeq(ctx, q, orgID, prefix, year)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d-%06d", prefix, year, n), nil
}

// NextCategorized: AST-HVAC-000001 (Naming Convention §12).
func NextCategorized(ctx context.Context, q db.Querier, orgID uuid.UUID, prefix, category string) (string, error) {
	n, err := nextSeq(ctx, q, orgID, prefix+"-"+category, 0)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%06d", prefix, category, n), nil
}

// NextPlain: PROP-000001, USR-000001, CHK-000001.
func NextPlain(ctx context.Context, q db.Querier, orgID uuid.UUID, prefix string) (string, error) {
	n, err := nextSeq(ctx, q, orgID, prefix, 0)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%06d", prefix, n), nil
}

const base32Alphabet = "ABCDEFGHJKMNPQRSTVWXYZ0123456789" // tanpa I, L, O, U agar mudah dibaca

// NewQRCode: 12 karakter base32 opaque (TAD §8.6).
func NewQRCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 12)
	for i := range b {
		out[i] = base32Alphabet[int(b[i])%len(base32Alphabet)]
	}
	return string(out), nil
}
