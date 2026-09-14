package seed

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// seedDemoOperations: assets, checklist templates, maintenance plan, patrol route, cleaning schedule, contoh WO/Task/SR.
// Diisi bertahap seiring modul selesai.
func SeedDemoOperations(ctx context.Context, tx pgx.Tx, refs *DemoRefs) error {
	return nil
}
