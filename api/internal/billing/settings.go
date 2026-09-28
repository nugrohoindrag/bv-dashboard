package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// ---------- Pengaturan billing (PRD P4 v2.1 P4-INV-03, P4-VRF-04, P4-COL-02; D-P4-02) ----------
// Default organization (property_id NULL) dengan override per property. Dikelola Finance (billing.settings.manage) — bukan
// lagi izin organisasi (B-15).

type Settings struct {
	PropertyID          *uuid.UUID `json:"property_id"`
	Inherited           bool       `json:"inherited"` // true = belum ada override property; nilai dari default organization
	TaxEnabled          bool       `json:"tax_enabled"`
	TaxName             string     `json:"tax_name"`
	TaxRate             float64    `json:"tax_rate"`
	SellerName          *string    `json:"seller_name"`
	SellerTaxID         *string    `json:"seller_tax_id"`
	SellerAddress       *string    `json:"seller_address"`
	InvoiceFooter       *string    `json:"invoice_footer"`
	PaymentInstructions *string    `json:"payment_instructions"`
	DefaultDueDays      int        `json:"default_due_days"`
	ReminderOffsets     []int32    `json:"reminder_offsets"`
	UpdatedAt           *time.Time `json:"updated_at"`
	Version             int        `json:"version"`
}

func defaultSettings() Settings {
	return Settings{TaxName: "PPN", TaxRate: 11, DefaultDueDays: 14, ReminderOffsets: []int32{-3, 1, 7, 14, 30}, Inherited: true}
}

// settingsFor: override property → default organization → default sistem.
func (s *Service) settingsFor(ctx context.Context, tx pgx.Tx, propertyID *uuid.UUID) Settings {
	st := defaultSettings()
	q := `SELECT property_id, tax_enabled, tax_name, tax_rate::float8, seller_name, seller_tax_id, seller_address, invoice_footer, payment_instructions, default_due_days, reminder_offsets, updated_at, version
		FROM billing_settings WHERE (property_id = $1 OR property_id IS NULL) ORDER BY property_id NULLS LAST LIMIT 1`
	var pid *uuid.UUID
	var upd time.Time
	err := tx.QueryRow(ctx, q, propertyID).Scan(&pid, &st.TaxEnabled, &st.TaxName, &st.TaxRate, &st.SellerName, &st.SellerTaxID, &st.SellerAddress, &st.InvoiceFooter, &st.PaymentInstructions, &st.DefaultDueDays, &st.ReminderOffsets, &upd, &st.Version)
	if err == nil {
		st.UpdatedAt = &upd
		st.Inherited = propertyID != nil && (pid == nil || *pid != *propertyID)
	}
	st.PropertyID = propertyID
	return st
}

func (s *Service) GetSettings(ctx context.Context, propertyID *uuid.UUID) (*Settings, error) {
	p := authctx.Must(ctx)
	if propertyID != nil && !p.HasAnyOnProperty("billing.settings.view", *propertyID) {
		return nil, apperr.Forbidden("Memerlukan billing.settings.view")
	}
	var out Settings
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		out = s.settingsFor(ctx, tx, propertyID)
		return nil
	})
	return &out, err
}

type SettingsInput struct {
	TaxEnabled          *bool    `json:"tax_enabled"`
	TaxName             *string  `json:"tax_name"`
	TaxRate             *float64 `json:"tax_rate"`
	SellerName          *string  `json:"seller_name"`
	SellerTaxID         *string  `json:"seller_tax_id"`
	SellerAddress       *string  `json:"seller_address"`
	InvoiceFooter       *string  `json:"invoice_footer"`
	PaymentInstructions *string  `json:"payment_instructions"`
	DefaultDueDays      *int     `json:"default_due_days"`
	ReminderOffsets     *[]int32 `json:"reminder_offsets"`
}

// PutSettings: upsert default org (property_id kosong; butuh grant tingkat organization) atau override property.
func (s *Service) PutSettings(ctx context.Context, propertyID *uuid.UUID, in SettingsInput) (*Settings, error) {
	p := authctx.Must(ctx)
	if propertyID != nil {
		if !p.HasOnProperty("billing.settings.manage", *propertyID) {
			return nil, apperr.Forbidden("Memerlukan billing.settings.manage")
		}
	} else if _, all := p.PropertyIDsFor("billing.settings.manage"); !all {
		return nil, apperr.Forbidden("Pengaturan default organization memerlukan billing.settings.manage tingkat organization")
	}
	if in.TaxRate != nil && (*in.TaxRate < 0 || *in.TaxRate > 100) {
		return nil, apperr.Validation("tax_rate harus 0..100").WithField("tax_rate", "0..100")
	}
	if in.DefaultDueDays != nil && (*in.DefaultDueDays < 0 || *in.DefaultDueDays > 120) {
		return nil, apperr.Validation("default_due_days harus 0..120")
	}
	if in.ReminderOffsets != nil {
		for _, o := range *in.ReminderOffsets {
			if o < -30 || o > 180 {
				return nil, apperr.Validation("reminder_offsets harus -30..180 hari").WithField("reminder_offsets", "rentang -30..180")
			}
		}
	}
	var out Settings
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		cur := s.settingsFor(ctx, tx, propertyID)
		if in.TaxEnabled != nil {
			cur.TaxEnabled = *in.TaxEnabled
		}
		if in.TaxName != nil && strings.TrimSpace(*in.TaxName) != "" {
			cur.TaxName = strings.TrimSpace(*in.TaxName)
		}
		if in.TaxRate != nil {
			cur.TaxRate = *in.TaxRate
		}
		setStr := func(dst **string, v *string) {
			if v != nil {
				t := strings.TrimSpace(*v)
				if t == "" {
					*dst = nil
				} else {
					*dst = &t
				}
			}
		}
		setStr(&cur.SellerName, in.SellerName)
		setStr(&cur.SellerTaxID, in.SellerTaxID)
		setStr(&cur.SellerAddress, in.SellerAddress)
		setStr(&cur.InvoiceFooter, in.InvoiceFooter)
		setStr(&cur.PaymentInstructions, in.PaymentInstructions)
		if in.DefaultDueDays != nil {
			cur.DefaultDueDays = *in.DefaultDueDays
		}
		if in.ReminderOffsets != nil {
			cur.ReminderOffsets = *in.ReminderOffsets
		}
		if _, err := tx.Exec(ctx, `INSERT INTO billing_settings (organization_id, property_id, tax_enabled, tax_name, tax_rate, seller_name, seller_tax_id, seller_address, invoice_footer, payment_instructions, default_due_days, reminder_offsets, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (organization_id, COALESCE(property_id, '00000000-0000-0000-0000-000000000000'::uuid)) DO UPDATE SET tax_enabled = EXCLUDED.tax_enabled, tax_name = EXCLUDED.tax_name,
			  tax_rate = EXCLUDED.tax_rate, seller_name = EXCLUDED.seller_name, seller_tax_id = EXCLUDED.seller_tax_id, seller_address = EXCLUDED.seller_address, invoice_footer = EXCLUDED.invoice_footer,
			  payment_instructions = EXCLUDED.payment_instructions, default_due_days = EXCLUDED.default_due_days, reminder_offsets = EXCLUDED.reminder_offsets, updated_by = EXCLUDED.updated_by`,
			p.OrganizationID, propertyID, cur.TaxEnabled, cur.TaxName, cur.TaxRate, cur.SellerName, cur.SellerTaxID, cur.SellerAddress, cur.InvoiceFooter, cur.PaymentInstructions, cur.DefaultDueDays, cur.ReminderOffsets, p.UserID); err != nil {
			return err
		}
		label := "organization"
		if propertyID != nil {
			label = propertyID.String()
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "billing_settings", EntityLabel: label, After: in})
		out = s.settingsFor(ctx, tx, propertyID)
		return nil
	})
	return &out, err
}

// ---------- Rekening bank (instruksi transfer & rekonsiliasi, P4-VRF-04 / P4-REC-01) ----------

type BankAccount struct {
	ID            uuid.UUID  `json:"id"`
	PropertyID    *uuid.UUID `json:"property_id"`
	BankName      string     `json:"bank_name"`
	AccountNumber string     `json:"account_number"`
	AccountName   string     `json:"account_name"`
	Branch        *string    `json:"branch"`
	IsDefault     bool       `json:"is_default"`
	IsActive      bool       `json:"is_active"`
	Notes         *string    `json:"notes"`
	Version       int        `json:"version"`
}

const bankSelect = `SELECT id, property_id, bank_name, account_number, account_name, branch, is_default, is_active, notes, version FROM bank_accounts`

func scanBank(row pgx.Row) (*BankAccount, error) {
	var b BankAccount
	if err := row.Scan(&b.ID, &b.PropertyID, &b.BankName, &b.AccountNumber, &b.AccountName, &b.Branch, &b.IsDefault, &b.IsActive, &b.Notes, &b.Version); err != nil {
		return nil, err
	}
	return &b, nil
}

// bankAccountText: rekening aktif property (default lebih dulu; fallback rekening organization) untuk instruksi transfer.
func (s *Service) bankAccountText(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) string {
	rows, err := tx.Query(ctx, bankSelect+` WHERE is_active AND (property_id = $1 OR property_id IS NULL) ORDER BY property_id NULLS LAST, is_default DESC, bank_name LIMIT 3`, propertyID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		b, err := scanBank(rows)
		if err != nil {
			return ""
		}
		parts = append(parts, fmt.Sprintf("%s %s a.n. %s", b.BankName, b.AccountNumber, b.AccountName))
	}
	return strings.Join(parts, "; ")
}

func (s *Service) ListBankAccounts(ctx context.Context, propertyID *uuid.UUID) ([]BankAccount, error) {
	p := authctx.Must(ctx)
	out := []BankAccount{}
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		where := ` WHERE true`
		args := []any{}
		if propertyID != nil {
			if !p.HasAnyOnProperty("billing.settings.view", *propertyID) && !p.HasAnyOnProperty("billing.reconciliation.view", *propertyID) {
				return apperr.Forbidden("")
			}
			args = append(args, *propertyID)
			where += ` AND (property_id = $1 OR property_id IS NULL)`
		} else if pids, all := p.PropertyIDsFor("billing.settings.view"); !all {
			args = append(args, pids)
			where += ` AND (property_id IS NULL OR property_id = ANY($1))`
		}
		rows, err := tx.Query(ctx, bankSelect+where+` ORDER BY property_id NULLS FIRST, is_default DESC, bank_name`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBank(rows)
			if err != nil {
				return err
			}
			out = append(out, *b)
		}
		return rows.Err()
	})
	return out, err
}

type BankAccountInput struct {
	PropertyID    *uuid.UUID `json:"property_id"`
	BankName      *string    `json:"bank_name"`
	AccountNumber *string    `json:"account_number"`
	AccountName   *string    `json:"account_name"`
	Branch        *string    `json:"branch"`
	IsDefault     *bool      `json:"is_default"`
	IsActive      *bool      `json:"is_active"`
	Notes         *string    `json:"notes"`
}

func (s *Service) SaveBankAccount(ctx context.Context, id *uuid.UUID, in BankAccountInput) (*BankAccount, error) {
	p := authctx.Must(ctx)
	var out *BankAccount
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		propertyID := in.PropertyID
		if id != nil {
			cur, err := scanBank(tx.QueryRow(ctx, bankSelect+` WHERE id = $1`, *id))
			if err != nil {
				return apperr.NotFound("Rekening")
			}
			propertyID = cur.PropertyID
		}
		if propertyID != nil {
			if !p.HasOnProperty("billing.settings.manage", *propertyID) {
				return apperr.Forbidden("Memerlukan billing.settings.manage")
			}
		} else if _, all := p.PropertyIDsFor("billing.settings.manage"); !all {
			return apperr.Forbidden("Rekening organization memerlukan billing.settings.manage tingkat organization")
		}
		var bid uuid.UUID
		if id == nil {
			if strings.TrimSpace(deref(in.BankName)) == "" || strings.TrimSpace(deref(in.AccountNumber)) == "" || strings.TrimSpace(deref(in.AccountName)) == "" {
				return apperr.Validation("bank_name, account_number, account_name wajib")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO bank_accounts (organization_id, property_id, bank_name, account_number, account_name, branch, is_default, is_active, notes, created_by, updated_by)
				VALUES ($1,$2,$3,$4,$5,NULLIF(TRIM($6),''),COALESCE($7,false),COALESCE($8,true),$9,$10,$10) RETURNING id`,
				p.OrganizationID, propertyID, strings.TrimSpace(deref(in.BankName)), strings.TrimSpace(deref(in.AccountNumber)), strings.TrimSpace(deref(in.AccountName)), deref(in.Branch), in.IsDefault, in.IsActive, in.Notes, p.UserID).Scan(&bid); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("BANK_ACCOUNT_EXISTS", "Nomor rekening sudah terdaftar")
				}
				return err
			}
		} else {
			bid = *id
			if _, err := tx.Exec(ctx, `UPDATE bank_accounts SET bank_name = COALESCE(NULLIF(TRIM($2),''), bank_name), account_number = COALESCE(NULLIF(TRIM($3),''), account_number),
				account_name = COALESCE(NULLIF(TRIM($4),''), account_name), branch = COALESCE($5, branch), is_default = COALESCE($6, is_default), is_active = COALESCE($7, is_active), notes = COALESCE($8, notes), updated_by = $9 WHERE id = $1`,
				bid, deref(in.BankName), deref(in.AccountNumber), deref(in.AccountName), in.Branch, in.IsDefault, in.IsActive, in.Notes, p.UserID); err != nil {
				return err
			}
		}
		if in.IsDefault != nil && *in.IsDefault {
			_, _ = tx.Exec(ctx, `UPDATE bank_accounts SET is_default = false WHERE id <> $1 AND property_id IS NOT DISTINCT FROM $2`, bid, propertyID)
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditConfigChange, EntityType: "bank_account", EntityID: &bid, EntityLabel: deref(in.BankName) + " " + deref(in.AccountNumber)})
		var err error
		out, err = scanBank(tx.QueryRow(ctx, bankSelect+` WHERE id = $1`, bid))
		return err
	})
	return out, err
}
