package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
)

// Idempotency (TAD §6.4): POST dengan header Idempotency-Key mengembalikan response tersimpan (24 jam)
// untuk retry; key sama dengan body berbeda → 422.
func Idempotency(d *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(HeaderIdempotencyKey)
			if key == "" || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			p, ok := authctx.From(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if _, err := uuid.Parse(key); err != nil || len(key) > 128 {
				WriteError(w, r, apperr.Validation("Idempotency-Key harus UUID"))
				return
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
			r.Body = io.NopCloser(bytes.NewReader(body))
			sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
			hash := hex.EncodeToString(sum[:])

			var status int
			var stored []byte
			var storedHash string
			err := d.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT status_code, response_body, request_hash FROM idempotency_keys WHERE organization_id = $1 AND user_id = $2 AND key = $3 AND expires_at > now()`, p.OrganizationID, p.UserID, key).Scan(&status, &stored, &storedHash)
			})
			if err == nil {
				if storedHash != hash {
					WriteError(w, r, apperr.New(422, "IDEMPOTENCY_MISMATCH", "Idempotency key reused", "Idempotency-Key sudah dipakai untuk request berbeda"))
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(status)
				_, _ = w.Write(stored)
				return
			}
			rec := &captureWriter{ResponseWriter: w, buf: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 300 && json.Valid(rec.buf.Bytes()) {
				_ = d.WithTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
					_, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (organization_id, user_id, key, request_hash, status_code, response_body) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
						p.OrganizationID, p.UserID, key, hash, rec.status, rec.buf.Bytes())
					return err
				})
			}
		})
	}
}

type captureWriter struct {
	http.ResponseWriter
	status int
	buf    *bytes.Buffer
}

func (c *captureWriter) WriteHeader(code int) { c.status = code; c.ResponseWriter.WriteHeader(code) }
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}
