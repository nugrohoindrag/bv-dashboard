package operations

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/audit"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/events"
	"github.com/buildingvision/api/internal/platform/ids"
)

// ---------- Checklist Template (PRD §11.1; authoring draft|published|archived) ----------

type ChecklistTemplateItem struct {
	ID            *uuid.UUID `json:"id,omitempty"`
	SortOrder     int        `json:"sort_order"`
	Section       *string    `json:"section"`
	Label         string     `json:"label"`
	ItemType      string     `json:"item_type"` // ok_notok_na | yes_no | numeric | text | photo
	IsRequired    bool       `json:"is_required"`
	PhotoRequired bool       `json:"photo_required"`
	NumericUnit   *string    `json:"numeric_unit"`
	NumericMin    *float64   `json:"numeric_min"`
	NumericMax    *float64   `json:"numeric_max"`
	HelpText      *string    `json:"help_text"`
}

type ChecklistTemplate struct {
	ID             uuid.UUID               `json:"id"`
	Code           string                  `json:"code"`
	Name           string                  `json:"name"`
	Description    *string                 `json:"description"`
	Domain         *string                 `json:"domain"`
	AppliesTo      []string                `json:"applies_to"`
	Status         string                  `json:"status"`
	CurrentVersion int                     `json:"current_version"`
	Items          []ChecklistTemplateItem `json:"items"`
	UsageCount     int                     `json:"usage_count"`
	UpdatedAt      time.Time               `json:"updated_at"`
	Version        int                     `json:"version"`
}

type ChecklistTemplateInput struct {
	Name        *string                  `json:"name"`
	Description *string                  `json:"description"`
	Domain      *string                  `json:"domain"`
	AppliesTo   *[]string                `json:"applies_to"`
	Items       *[]ChecklistTemplateItem `json:"items"`
}

var itemTypes = map[string]bool{"ok_notok_na": true, "yes_no": true, "numeric": true, "text": true, "photo": true}

func (s *Service) CreateChecklistTemplate(ctx context.Context, in ChecklistTemplateInput) (*ChecklistTemplate, error) {
	p := authctx.Must(ctx)
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("name wajib")
	}
	var out *ChecklistTemplate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		code, err := ids.NextPlain(ctx, tx, p.OrganizationID, ids.PrefixChecklist)
		if err != nil {
			return err
		}
		applies := []string{}
		if in.AppliesTo != nil {
			applies = *in.AppliesTo
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO checklist_templates (organization_id, code, name, description, domain, applies_to, created_by, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`,
			p.OrganizationID, code, strings.TrimSpace(*in.Name), in.Description, in.Domain, applies, p.UserID).Scan(&id); err != nil {
			return err
		}
		if in.Items != nil {
			if err := s.replaceTemplateItems(ctx, tx, id, 1, *in.Items); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditCreate, EntityType: "checklist_template", EntityID: &id, EntityLabel: code, After: in})
		out, err = s.getTemplateTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) replaceTemplateItems(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, version int, items []ChecklistTemplateItem) error {
	p := authctx.Must(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM checklist_template_items WHERE template_id = $1 AND template_version = $2`, templateID, version); err != nil {
		return err
	}
	for i, it := range items {
		if strings.TrimSpace(it.Label) == "" {
			return apperr.Validation("label item wajib")
		}
		if !itemTypes[it.ItemType] {
			return apperr.Validation("item_type tidak valid: " + it.ItemType)
		}
		if it.ItemType == "photo" {
			it.PhotoRequired = true
		}
		if _, err := tx.Exec(ctx, `INSERT INTO checklist_template_items (organization_id, template_id, template_version, sort_order, section, label, item_type, is_required, photo_required, numeric_unit, numeric_min, numeric_max, help_text)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			p.OrganizationID, templateID, version, i, it.Section, strings.TrimSpace(it.Label), it.ItemType, it.IsRequired, it.PhotoRequired, it.NumericUnit, it.NumericMin, it.NumericMax, it.HelpText); err != nil {
			return err
		}
	}
	return nil
}

// UpdateChecklistTemplate: perubahan item pada template published membuat versi baru (snapshot per run tetap aman, R-06).
func (s *Service) UpdateChecklistTemplate(ctx context.Context, id uuid.UUID, in ChecklistTemplateInput) (*ChecklistTemplate, error) {
	p := authctx.Must(ctx)
	var out *ChecklistTemplate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		before, err := s.getTemplateTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status == "archived" {
			return apperr.Conflict("TEMPLATE_ARCHIVED", "Template sudah diarsipkan")
		}
		var applies *[]string = in.AppliesTo
		if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET name = COALESCE(NULLIF($2,''), name), description = COALESCE($3, description), domain = COALESCE($4, domain), applies_to = COALESCE($5, applies_to), updated_by = $6 WHERE id = $1`,
			id, derefStr(in.Name), in.Description, in.Domain, applies, p.UserID); err != nil {
			return err
		}
		if in.Items != nil {
			ver := before.CurrentVersion
			if before.Status == "published" {
				ver++
				if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET current_version = $2 WHERE id = $1`, id, ver); err != nil {
					return err
				}
			}
			if err := s.replaceTemplateItems(ctx, tx, id, ver, *in.Items); err != nil {
				return err
			}
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditUpdate, EntityType: "checklist_template", EntityID: &id, EntityLabel: before.Code, Before: before, After: in})
		out, err = s.getTemplateTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) SetChecklistTemplateStatus(ctx context.Context, id uuid.UUID, status string) (*ChecklistTemplate, error) {
	p := authctx.Must(ctx)
	if status != "published" && status != "archived" && status != "draft" {
		return nil, apperr.Validation("status harus draft|published|archived")
	}
	var out *ChecklistTemplate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.getTemplateTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if status == "published" && len(t.Items) == 0 {
			return apperr.Validation("template tanpa item tidak dapat dipublikasikan")
		}
		if _, err := tx.Exec(ctx, `UPDATE checklist_templates SET status = $2, updated_by = $3 WHERE id = $1`, id, status, p.UserID); err != nil {
			return err
		}
		_ = audit.Log(ctx, tx, audit.AuditEntry{Action: audit.AuditStatusChange, EntityType: "checklist_template", EntityID: &id, EntityLabel: t.Code, Before: map[string]any{"status": t.Status}, After: map[string]any{"status": status}})
		out, err = s.getTemplateTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) getTemplateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*ChecklistTemplate, error) {
	var t ChecklistTemplate
	if err := tx.QueryRow(ctx, `SELECT id, code, name, description, domain, applies_to, status, current_version, updated_at, version,
		(SELECT count(*) FROM checklist_runs r WHERE r.template_id = checklist_templates.id)
		FROM checklist_templates WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Domain, &t.AppliesTo, &t.Status, &t.CurrentVersion, &t.UpdatedAt, &t.Version, &t.UsageCount); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Checklist template")
		}
		return nil, err
	}
	if t.AppliesTo == nil {
		t.AppliesTo = []string{}
	}
	t.Items = []ChecklistTemplateItem{}
	rows, err := tx.Query(ctx, `SELECT id, sort_order, section, label, item_type, is_required, photo_required, numeric_unit, numeric_min, numeric_max, help_text
		FROM checklist_template_items WHERE template_id = $1 AND template_version = $2 ORDER BY sort_order`, id, t.CurrentVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ChecklistTemplateItem
		var iid uuid.UUID
		if err := rows.Scan(&iid, &it.SortOrder, &it.Section, &it.Label, &it.ItemType, &it.IsRequired, &it.PhotoRequired, &it.NumericUnit, &it.NumericMin, &it.NumericMax, &it.HelpText); err != nil {
			return nil, err
		}
		it.ID = &iid
		t.Items = append(t.Items, it)
	}
	return &t, rows.Err()
}

func (s *Service) GetChecklistTemplate(ctx context.Context, id uuid.UUID) (*ChecklistTemplate, error) {
	var out *ChecklistTemplate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.getTemplateTx(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Service) ListChecklistTemplates(ctx context.Context, status, domain, appliesTo, q string) ([]ChecklistTemplate, error) {
	var out []ChecklistTemplate
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM checklist_templates WHERE deleted_at IS NULL AND ($1 = '' OR status = $1) AND ($2 = '' OR domain = $2) AND ($3 = '' OR $3 = ANY(applies_to)) AND ($4 = '' OR name ILIKE '%' || $4 || '%' OR code ILIKE '%' || $4 || '%') ORDER BY name`, status, domain, appliesTo, q)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			t, err := s.getTemplateTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return nil
	})
	if out == nil {
		out = []ChecklistTemplate{}
	}
	return out, err
}

// ---------- Checklist Run (snapshot template per run, TAD §7.4) ----------

type ChecklistRunItem struct {
	ID             uuid.UUID  `json:"id"`
	SortOrder      int        `json:"sort_order"`
	Section        *string    `json:"section"`
	Label          string     `json:"label"`
	ItemType       string     `json:"item_type"`
	IsRequired     bool       `json:"is_required"`
	PhotoRequired  bool       `json:"photo_required"`
	NumericUnit    *string    `json:"numeric_unit"`
	NumericMin     *float64   `json:"numeric_min"`
	NumericMax     *float64   `json:"numeric_max"`
	ResultValue    *string    `json:"result_value"`
	ResultNumber   *float64   `json:"result_number"`
	ResultText     *string    `json:"result_text"`
	AttachmentID   *uuid.UUID `json:"attachment_id"`
	Note           *string    `json:"note"`
	AnsweredBy     *uuid.UUID `json:"answered_by"`
	AnsweredByName *string    `json:"answered_by_name"`
	AnsweredAt     *time.Time `json:"answered_at"`
	AnsweredSource *string    `json:"answered_source"`
	FindingID      *uuid.UUID `json:"finding_id"`
	OutOfRange     bool       `json:"out_of_range"`
}

type ChecklistRun struct {
	ID              uuid.UUID          `json:"id"`
	ObjectType      string             `json:"object_type"`
	ObjectID        uuid.UUID          `json:"object_id"`
	TemplateID      uuid.UUID          `json:"template_id"`
	TemplateVersion int                `json:"template_version"`
	TemplateName    string             `json:"template_name"`
	Status          string             `json:"status"`
	TotalItems      int                `json:"total_items"`
	AnsweredItems   int                `json:"answered_items"`
	NotOKItems      int                `json:"not_ok_items"`
	StartedAt       *time.Time         `json:"started_at"`
	CompletedAt     *time.Time         `json:"completed_at"`
	Items           []ChecklistRunItem `json:"items"`
}

// startChecklistRunTx: snapshot template ke run (dipanggil saat object dibuat / template diubah).
func (s *Service) startChecklistRunTx(ctx context.Context, tx pgx.Tx, objectType string, objectID, templateID uuid.UUID) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	// bila sudah ada run untuk template & version yang sama → pakai yang ada
	var existing uuid.UUID
	var ver int
	var name string
	if err := tx.QueryRow(ctx, `SELECT current_version, name FROM checklist_templates WHERE id = $1 AND deleted_at IS NULL`, templateID).Scan(&ver, &name); err != nil {
		return uuid.Nil, apperr.Validation("checklist template tidak ditemukan")
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM checklist_runs WHERE object_type = $1 AND object_id = $2 AND template_id = $3 AND template_version = $4`, objectType, objectID, templateID, ver).Scan(&existing); err == nil {
		return existing, nil
	}
	var runID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO checklist_runs (organization_id, object_type, object_id, template_id, template_version, template_name, status) VALUES ($1,$2,$3,$4,$5,$6,'pending') RETURNING id`,
		p.OrganizationID, objectType, objectID, templateID, ver, name).Scan(&runID); err != nil {
		return uuid.Nil, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO checklist_run_items (organization_id, run_id, template_item_id, sort_order, section, label, item_type, is_required, photo_required, numeric_unit, numeric_min, numeric_max)
		SELECT $1, $2, id, sort_order, section, label, item_type, is_required, photo_required, numeric_unit, numeric_min, numeric_max FROM checklist_template_items WHERE template_id = $3 AND template_version = $4 ORDER BY sort_order`,
		p.OrganizationID, runID, templateID, ver)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE checklist_runs SET total_items = $2 WHERE id = $1`, runID, tag.RowsAffected()); err != nil {
		return uuid.Nil, err
	}
	// hanya satu run aktif per object: run lama (template lain/versi lama) yang belum dijawab dihapus
	_, _ = tx.Exec(ctx, `DELETE FROM checklist_runs WHERE object_type = $1 AND object_id = $2 AND id <> $3 AND answered_items = 0`, objectType, objectID, runID)
	return runID, nil
}

// AttachChecklist: pasang template ke object (task/WO) → run.
func (s *Service) AttachChecklist(ctx context.Context, objectType string, objectID, templateID uuid.UUID) (*ChecklistRun, error) {
	var out *ChecklistRun
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ObjectAccess(ctx, tx, objectType, objectID, true); err != nil {
			return err
		}
		if t, e := tableFor(objectType); e == nil {
			if _, err := tx.Exec(ctx, `UPDATE `+t.table+` SET checklist_template_id = $2 WHERE id = $1`, objectID, templateID); err != nil {
				return err
			}
		}
		runID, err := s.startChecklistRunTx(ctx, tx, objectType, objectID, templateID)
		if err != nil {
			return err
		}
		_ = audit.Record(ctx, tx, audit.Entry{ObjectType: objectType, ObjectID: objectID, Action: audit.ActChecklistStarted, Payload: map[string]any{"template_id": templateID, "run_id": runID}})
		out, err = s.getRunTx(ctx, tx, runID)
		return err
	})
	return out, err
}

func (s *Service) getRunTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*ChecklistRun, error) {
	var r ChecklistRun
	if err := tx.QueryRow(ctx, `SELECT id, object_type, object_id, template_id, template_version, template_name, status, total_items, answered_items, not_ok_items, started_at, completed_at FROM checklist_runs WHERE id = $1`, runID).
		Scan(&r.ID, &r.ObjectType, &r.ObjectID, &r.TemplateID, &r.TemplateVersion, &r.TemplateName, &r.Status, &r.TotalItems, &r.AnsweredItems, &r.NotOKItems, &r.StartedAt, &r.CompletedAt); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Checklist run")
		}
		return nil, err
	}
	r.Items = []ChecklistRunItem{}
	rows, err := tx.Query(ctx, `SELECT i.id, i.sort_order, i.section, i.label, i.item_type, i.is_required, i.photo_required, i.numeric_unit, i.numeric_min, i.numeric_max,
		i.result_value, i.result_number, i.result_text, i.attachment_id, i.note, i.answered_by, u.full_name, i.answered_at, i.answered_source, i.finding_id
		FROM checklist_run_items i LEFT JOIN users u ON u.id = i.answered_by WHERE i.run_id = $1 ORDER BY i.sort_order`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ChecklistRunItem
		if err := rows.Scan(&it.ID, &it.SortOrder, &it.Section, &it.Label, &it.ItemType, &it.IsRequired, &it.PhotoRequired, &it.NumericUnit, &it.NumericMin, &it.NumericMax,
			&it.ResultValue, &it.ResultNumber, &it.ResultText, &it.AttachmentID, &it.Note, &it.AnsweredBy, &it.AnsweredByName, &it.AnsweredAt, &it.AnsweredSource, &it.FindingID); err != nil {
			return nil, err
		}
		if it.ResultNumber != nil && ((it.NumericMin != nil && *it.ResultNumber < *it.NumericMin) || (it.NumericMax != nil && *it.ResultNumber > *it.NumericMax)) {
			it.OutOfRange = true
		}
		r.Items = append(r.Items, it)
	}
	return &r, rows.Err()
}

// ListRuns untuk object.
func (s *Service) ListRuns(ctx context.Context, objectType string, objectID uuid.UUID) ([]ChecklistRun, error) {
	var out []ChecklistRun
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ObjectAccess(ctx, tx, objectType, objectID, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM checklist_runs WHERE object_type = $1 AND object_id = $2 ORDER BY created_at DESC`, objectType, objectID)
		if err != nil {
			return err
		}
		var idList []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			idList = append(idList, id)
		}
		rows.Close()
		for _, id := range idList {
			r, err := s.getRunTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return nil
	})
	if out == nil {
		out = []ChecklistRun{}
	}
	return out, err
}

func (s *Service) GetRun(ctx context.Context, runID uuid.UUID) (*ChecklistRun, error) {
	var out *ChecklistRun
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := s.getRunTx(ctx, tx, runID)
		if err != nil {
			return err
		}
		if err := s.ObjectAccess(ctx, tx, r.ObjectType, r.ObjectID, false); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}

type AnswerInput struct {
	ResultValue      *string    `json:"result_value"` // ok | not_ok | na | yes | no
	ResultNumber     *float64   `json:"result_number"`
	ResultText       *string    `json:"result_text"`
	AttachmentID     *uuid.UUID `json:"attachment_id"`
	Note             *string    `json:"note"`
	ClientRecordedAt *time.Time `json:"client_recorded_at"`
	CreateFinding    *bool      `json:"create_finding"` // not_ok → Finding otomatis (PRD §12.4, §15.2)
	FindingSeverity  *string    `json:"finding_severity"`
	FromSync         bool       `json:"-"`
}

// AnswerItem: jawaban checklist (C7: jawaban ulang = nilai baru; jawaban lama tetap di activity).
func (s *Service) AnswerItem(ctx context.Context, itemID uuid.UUID, in AnswerInput) (*ChecklistRun, error) {
	var out *ChecklistRun
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		runID, err := s.AnswerItemTx(ctx, tx, itemID, in)
		if err != nil {
			return err
		}
		out, err = s.getRunTx(ctx, tx, runID)
		return err
	})
	return out, err
}

func (s *Service) AnswerItemTx(ctx context.Context, tx pgx.Tx, itemID uuid.UUID, in AnswerInput) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	var runID, objectID uuid.UUID
	var objectType, itemType, label string
	var photoRequired bool
	var prevValue, prevText *string
	var prevNum *float64
	var propertyID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT i.run_id, r.object_type, r.object_id, i.item_type, i.label, i.photo_required, i.result_value, i.result_number, i.result_text
		FROM checklist_run_items i JOIN checklist_runs r ON r.id = i.run_id WHERE i.id = $1 FOR UPDATE OF i`, itemID).
		Scan(&runID, &objectType, &objectID, &itemType, &label, &photoRequired, &prevValue, &prevNum, &prevText)
	if err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, apperr.NotFound("Checklist item")
		}
		return uuid.Nil, err
	}
	if err := s.ObjectAccess(ctx, tx, objectType, objectID, true); err != nil {
		return uuid.Nil, err
	}
	// hanya assignee / manage yang boleh mengisi (executor); supervisor dengan manage boleh edit (DS §4.6)
	if t, e := tableFor(objectType); e == nil {
		w, _, err := s.loadTx(ctx, tx, objectType, objectID, false)
		if err != nil {
			return uuid.Nil, err
		}
		propertyID = &w.PropertyID
		gi := s.guardInput(ctx, tx, w, t, in.FromSync)
		if !gi.IsAssignee && !gi.HasManage {
			return uuid.Nil, apperr.Forbidden("Hanya assignee yang dapat mengisi checklist")
		}
		if w.Status != "in_progress" && !gi.HasManage {
			return uuid.Nil, apperr.Conflict("WORKFLOW_GUARD_FAILED", "Checklist hanya dapat diisi saat status In Progress (Start dulu)")
		}
	}
	// validasi per tipe
	switch itemType {
	case "ok_notok_na":
		if in.ResultValue == nil || (*in.ResultValue != "ok" && *in.ResultValue != "not_ok" && *in.ResultValue != "na") {
			return uuid.Nil, apperr.Validation("result_value harus ok|not_ok|na")
		}
	case "yes_no":
		if in.ResultValue == nil || (*in.ResultValue != "yes" && *in.ResultValue != "no") {
			return uuid.Nil, apperr.Validation("result_value harus yes|no")
		}
	case "numeric":
		if in.ResultNumber == nil {
			return uuid.Nil, apperr.Validation("result_number wajib")
		}
	case "text":
		if in.ResultText == nil || strings.TrimSpace(*in.ResultText) == "" {
			return uuid.Nil, apperr.Validation("result_text wajib")
		}
	case "photo":
		if in.AttachmentID == nil {
			return uuid.Nil, apperr.Validation("attachment_id wajib untuk item photo")
		}
	}
	if in.AttachmentID != nil {
		var ok bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE id = $1 AND deleted_at IS NULL)`, *in.AttachmentID).Scan(&ok)
		if !ok {
			return uuid.Nil, apperr.Validation("attachment_id tidak ditemukan")
		}
	}
	src := string(p.Source)
	if in.FromSync {
		src = "sync"
	}
	if src != "web" && src != "mobile" && src != "sync" {
		src = "web"
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE checklist_run_items SET result_value = $2, result_number = $3, result_text = $4, attachment_id = COALESCE($5, attachment_id), note = COALESCE($6, note), answered_by = $7, answered_at = $8, answered_source = $9 WHERE id = $1`,
		itemID, in.ResultValue, in.ResultNumber, in.ResultText, in.AttachmentID, in.Note, p.UserID, now, src); err != nil {
		return uuid.Nil, err
	}
	// recompute run counters
	if _, err := tx.Exec(ctx, `UPDATE checklist_runs r SET
		answered_items = (SELECT count(*) FROM checklist_run_items i WHERE i.run_id = r.id AND i.answered_at IS NOT NULL),
		not_ok_items = (SELECT count(*) FROM checklist_run_items i WHERE i.run_id = r.id AND (i.result_value = 'not_ok' OR i.result_value = 'no')),
		started_at = COALESCE(r.started_at, $2),
		status = CASE WHEN (SELECT count(*) FROM checklist_run_items i WHERE i.run_id = r.id AND i.answered_at IS NULL AND i.is_required) = 0 THEN 'completed' ELSE 'in_progress' END,
		completed_at = CASE WHEN (SELECT count(*) FROM checklist_run_items i WHERE i.run_id = r.id AND i.answered_at IS NULL AND i.is_required) = 0 THEN COALESCE(r.completed_at, $2) ELSE NULL END
		WHERE r.id = $1`, runID, now); err != nil {
		return uuid.Nil, err
	}
	payload := map[string]any{"item_id": itemID, "label": label, "value": in.ResultValue, "number": in.ResultNumber, "text": in.ResultText, "attachment_id": in.AttachmentID}
	if prevValue != nil || prevNum != nil || prevText != nil {
		payload["previous"] = map[string]any{"value": prevValue, "number": prevNum, "text": prevText}
	}
	ctxAct := ctx
	if in.FromSync {
		ctxAct = authctx.With(ctx, withSource(p, authctx.SourceSync))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: objectType, ObjectID: objectID, Action: audit.ActChecklistAnswered, Payload: payload, ClientRecordedAt: in.ClientRecordedAt})
	var status string
	_ = tx.QueryRow(ctx, `SELECT status FROM checklist_runs WHERE id = $1`, runID).Scan(&status)
	if status == "completed" {
		_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: objectType, ObjectID: objectID, Action: audit.ActChecklistCompleted, Payload: map[string]any{"run_id": runID}})
	}
	// Not OK → Finding (opsional, default: ya untuk inspection/patrol/cleaning)
	isNotOK := in.ResultValue != nil && (*in.ResultValue == "not_ok" || *in.ResultValue == "no")
	create := isNotOK && ((in.CreateFinding != nil && *in.CreateFinding) || (in.CreateFinding == nil && objectType == ObjTask))
	if create && propertyID != nil {
		var existing uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT finding_id FROM checklist_run_items WHERE id = $1 AND finding_id IS NOT NULL`, itemID).Scan(&existing); err != nil {
			sev := "medium"
			if in.FindingSeverity != nil && Priorities[*in.FindingSeverity] {
				sev = *in.FindingSeverity
			}
			var locID, assetID *uuid.UUID
			var ftype string = "checklist"
			if t, e := tableFor(objectType); e == nil {
				_ = tx.QueryRow(ctx, `SELECT location_id, asset_id FROM `+t.table+` WHERE id = $1`, objectID).Scan(&locID, &assetID)
				if objectType == ObjTask {
					var tt string
					_ = tx.QueryRow(ctx, `SELECT task_type FROM tasks WHERE id = $1`, objectID).Scan(&tt)
					switch tt {
					case "patrol":
						ftype = "patrol"
					case "inspection":
						ftype = "inspection"
						var hk bool
						_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM housekeeping_inspections WHERE task_id = $1)`, objectID).Scan(&hk)
						if hk {
							ftype = "housekeeping"
						}
					case "cleaning":
						ftype = "housekeeping"
					}
				}
			}
			desc := "Checklist item Not OK: " + label
			if in.Note != nil {
				desc += " — " + *in.Note
			}
			fid, err := s.CreateFindingTx(ctx, tx, CreateFindingInput{PropertyID: propertyID, FindingType: ftype, Title: label, Description: &desc, LocationID: locID, AssetID: assetID, Severity: sev,
				SourceType: strPtr(objectType), SourceID: &objectID, AttachmentID: in.AttachmentID})
			if err != nil {
				return uuid.Nil, err
			}
			_, _ = tx.Exec(ctx, `UPDATE checklist_run_items SET finding_id = $2 WHERE id = $1`, itemID, fid)
		}
	}
	return runID, nil
}

func strPtr(s string) *string { return &s }
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------- Comments (FR-TSK-008) ----------

func (s *Service) AddComment(ctx context.Context, objectType string, objectID uuid.UUID, body string, clientCommentID *string, clientRecordedAt *time.Time, fromSync bool) (*Comment, error) {
	var out *Comment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.AddCommentTx(ctx, tx, objectType, objectID, body, clientCommentID, clientRecordedAt, fromSync)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func (s *Service) AddCommentTx(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, body string, clientCommentID *string, clientRecordedAt *time.Time, fromSync bool) (*Comment, error) {
	p := authctx.Must(ctx)
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, apperr.Validation("body wajib")
	}
	if err := s.ObjectAccess(ctx, tx, objectType, objectID, false); err != nil {
		return nil, err
	}
	if clientCommentID != nil && *clientCommentID != "" {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM comments WHERE author_id = $1 AND client_comment_id = $2`, p.UserID, *clientCommentID).Scan(&id); err == nil {
			return s.getCommentTx(ctx, tx, id)
		}
	}
	src := string(p.Source)
	if fromSync {
		src = "sync"
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO comments (organization_id, object_type, object_id, author_id, body, source, client_comment_id) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		p.OrganizationID, objectType, objectID, p.UserID, body, src, clientCommentID).Scan(&id); err != nil {
		return nil, err
	}
	ctxAct := ctx
	if fromSync {
		ctxAct = authctx.With(ctx, withSource(p, authctx.SourceSync))
	}
	_ = audit.Record(ctxAct, tx, audit.Entry{ObjectType: objectType, ObjectID: objectID, Action: audit.ActCommented, Payload: map[string]any{"comment_id": id, "excerpt": excerpt(body, 140)}, ClientRecordedAt: clientRecordedAt})
	if s.Jobs != nil {
		var label string
		var prop *uuid.UUID
		if t, e := tableFor(objectType); e == nil {
			_ = tx.QueryRow(ctx, `SELECT `+t.numberCol+`, property_id FROM `+t.table+` WHERE id = $1`, objectID).Scan(&label, &prop)
		}
		_ = s.Jobs.EnqueueEventTx(ctx, tx, events.Event{Type: events.CommentAdded, OrganizationID: p.OrganizationID, PropertyID: prop, ObjectType: objectType, ObjectID: objectID, ObjectLabel: label, ActorUserID: &p.UserID, Payload: map[string]any{"comment_id": id}})
	}
	return s.getCommentTx(ctx, tx, id)
}

func excerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Service) getCommentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Comment, error) {
	var c Comment
	err := tx.QueryRow(ctx, `SELECT c.id, c.object_type, c.object_id, c.author_id, u.full_name, c.body, c.source, c.created_at, c.edited_at FROM comments c JOIN users u ON u.id = c.author_id WHERE c.id = $1 AND c.deleted_at IS NULL`, id).
		Scan(&c.ID, &c.ObjectType, &c.ObjectID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Source, &c.CreatedAt, &c.EditedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) ListComments(ctx context.Context, objectType string, objectID uuid.UUID) ([]Comment, error) {
	var out []Comment
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ObjectAccess(ctx, tx, objectType, objectID, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.id, c.object_type, c.object_id, c.author_id, u.full_name, c.body, c.source, c.created_at, c.edited_at FROM comments c JOIN users u ON u.id = c.author_id WHERE c.object_type = $1 AND c.object_id = $2 AND c.deleted_at IS NULL ORDER BY c.created_at`, objectType, objectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Comment
			if err := rows.Scan(&c.ID, &c.ObjectType, &c.ObjectID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Source, &c.CreatedAt, &c.EditedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Comment{}
	}
	return out, err
}

// ---------- Object links (FR-TSK-013; SR ↔ WO bidirectional PRD §16.3) ----------

var linkTypes = map[string]bool{"generated_from": true, "related_to": true, "rework_of": true, "escalated_to": true}

func (s *Service) Link(ctx context.Context, fromType string, fromID uuid.UUID, toType string, toID uuid.UUID, linkType string) ([]ObjectLink, error) {
	var out []ObjectLink
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.ObjectAccess(ctx, tx, fromType, fromID, false); err != nil {
			return err
		}
		if err := s.ObjectAccess(ctx, tx, toType, toID, false); err != nil {
			return err
		}
		if _, err := s.linkTx(ctx, tx, fromType, fromID, toType, toID, linkType); err != nil {
			return err
		}
		var err error
		out, err = s.listLinksTx(ctx, tx, fromType, fromID)
		return err
	})
	return out, err
}

func (s *Service) linkTx(ctx context.Context, tx pgx.Tx, fromType string, fromID uuid.UUID, toType string, toID uuid.UUID, linkType string) (uuid.UUID, error) {
	p := authctx.Must(ctx)
	if linkType == "" {
		linkType = "related_to"
	}
	if !linkTypes[linkType] {
		return uuid.Nil, apperr.Validation("link_type tidak valid")
	}
	if fromType == toType && fromID == toID {
		return uuid.Nil, apperr.Validation("tidak dapat me-link object ke dirinya sendiri")
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO object_links (organization_id, from_type, from_id, to_type, to_id, link_type, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT DO NOTHING RETURNING id`, p.OrganizationID, fromType, fromID, toType, toID, linkType, actorOrNil(p)).Scan(&id)
	if err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, nil // sudah ada
		}
		return uuid.Nil, err
	}
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: fromType, ObjectID: fromID, Action: audit.ActLinked, Payload: map[string]any{"to_type": toType, "to_id": toID, "link_type": linkType}})
	_ = audit.Record(ctx, tx, audit.Entry{ObjectType: toType, ObjectID: toID, Action: audit.ActLinked, Payload: map[string]any{"from_type": fromType, "from_id": fromID, "link_type": linkType}})
	return id, nil
}

// LinkTx diekspor untuk modul lain.
func (s *Service) LinkTx(ctx context.Context, tx pgx.Tx, fromType string, fromID uuid.UUID, toType string, toID uuid.UUID, linkType string) error {
	_, err := s.linkTx(ctx, tx, fromType, fromID, toType, toID, linkType)
	return err
}

func (s *Service) listLinksTx(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID) ([]ObjectLink, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, link_type, 'to' AS direction, to_type AS ot, to_id AS oid FROM object_links WHERE from_type = $1 AND from_id = $2
		UNION ALL
		SELECT id, link_type, 'from', from_type, from_id FROM object_links WHERE to_type = $1 AND to_id = $2`, objectType, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ObjectLink{}
	for rows.Next() {
		var l ObjectLink
		if err := rows.Scan(&l.ID, &l.LinkType, &l.Direction, &l.ObjectType, &l.ObjectID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Label, out[i].Title, out[i].Status = describeObject(ctx, tx, out[i].ObjectType, out[i].ObjectID)
	}
	return out, nil
}

// ListLinks diekspor.
func (s *Service) ListLinks(ctx context.Context, objectType string, objectID uuid.UUID) ([]ObjectLink, error) {
	var out []ObjectLink
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.listLinksTx(ctx, tx, objectType, objectID)
		return err
	})
	return out, err
}

// describeObject: (business id, title, status) untuk object apa pun.
func describeObject(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) (label, title, status string) {
	q := map[string]string{
		ObjTask:                `SELECT task_number, title, status FROM tasks WHERE id = $1`,
		ObjWorkOrder:           `SELECT work_order_number, title, status FROM work_orders WHERE id = $1`,
		ObjServiceRequest:      `SELECT request_number, title, status FROM service_requests WHERE id = $1`,
		ObjIncident:            `SELECT incident_number, title, status FROM incidents WHERE id = $1`,
		ObjFinding:             `SELECT finding_number, title, status FROM findings WHERE id = $1`,
		ObjInspection:          `SELECT i.inspection_number, t.title, t.status FROM inspections i JOIN tasks t ON t.id = i.task_id WHERE i.task_id = $1`,
		ObjAsset:               `SELECT asset_code, name, status FROM assets WHERE id = $1`,
		ObjMaintenanceSchedule: `SELECT p.plan_code || ' ' || ms.due_date::text, p.name, ms.status FROM maintenance_schedules ms JOIN maintenance_plans p ON p.id = ms.plan_id WHERE ms.id = $1`,
	}[objectType]
	if q == "" {
		return objectType, "", ""
	}
	_ = tx.QueryRow(ctx, q, id).Scan(&label, &title, &status)
	return
}

// DescribeObject diekspor (search, notification).
func DescribeObject(ctx context.Context, tx pgx.Tx, objectType string, id uuid.UUID) (label, title, status string) {
	return describeObject(ctx, tx, objectType, id)
}
