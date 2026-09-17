// Package bvrooms: BVRooms — Customer Booking App (BVRooms-Backend-Requirements v0.2).
// Channel direct-booking white-label per organization (D1): customer umum memesan kamar hotel (inventori hotel_*)
// atau menyewa harian unit apartemen (bvrooms_unit_types → units.rentable_daily). Reservasi keduanya ditulis ke
// hotel_reservations (source=bvrooms) sehingga satu mesin status dengan Front Office. Pembayaran provider-agnostic,
// fase 1 manual_transfer / pay_at_property (D2, gateway ON HOLD). OTP SMS disiapkan dengan provider mock (D3).
package bvrooms

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/hotel"
	"github.com/buildingvision/api/internal/iam"
	"github.com/buildingvision/api/internal/platform/apperr"
	"github.com/buildingvision/api/internal/platform/authctx"
	"github.com/buildingvision/api/internal/platform/db"
	"github.com/buildingvision/api/internal/platform/jobs"
	"github.com/buildingvision/api/internal/platform/storage"
	"github.com/buildingvision/api/internal/profile"
)

const (
	EventBookingCreated   = "bvrooms_booking.created"
	EventBookingPaid      = "bvrooms_booking.paid"
	EventBookingCancelled = "bvrooms_booking.cancelled"
	EventBookingExpired   = "bvrooms_booking.expired"
	EventPaymentProof     = "bvrooms_payment.proof_submitted"
)

// Config: parameter runtime modul (dari config aplikasi).
type Config struct {
	Env            string        // local | test | staging | production — dev_code OTP hanya di local/test
	PublicURL      string        // untuk deep link
	AccessTTL      time.Duration // default 15 menit
	RefreshTTL     time.Duration // default 30 hari
	OTPTTL         time.Duration // default 100 detik (Figma 1:39)
	OTPResend      time.Duration // default 60 detik
	VAPIDPublicKey string        // dikirim ke client (pushManager.subscribe); kosong = Web Push nonaktif
	// Vendor SMS di-hold (D3): OTPStaticCode = kode tetap untuk semua nomor (mis. "1234", hanya demo/pilot — nonaktifkan saat vendor
	// terpasang); OTPExposeCode = kembalikan dev_code di respons walau bukan env local (kode tampil di layar).
	OTPStaticCode string
	OTPExposeCode bool
}

type Service struct {
	DB      *db.DB
	Jobs    jobs.Enqueuer
	Storage storage.Storage
	Hotel   *hotel.Service
	Profile *profile.Service
	SMS     SMSProvider
	Pusher  Pusher
	Log     *slog.Logger
	Cfg     Config

	signer     *customerSigner
	orgCache   sync.Map // slug → orgEntry
	ipLimiter  *ipLimiter
	nowFn      func() time.Time
	uploadTTL  time.Duration
	downloadTT time.Duration
}

type orgEntry struct {
	id     uuid.UUID
	name   string
	loaded time.Time
}

func New(d *db.DB, j jobs.Enqueuer, store storage.Storage, hotelSvc *hotel.Service, prof *profile.Service, signer *iam.TokenSigner, log *slog.Logger, cfg Config) *Service {
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.OTPTTL == 0 {
		cfg.OTPTTL = 100 * time.Second
	}
	if cfg.OTPResend == 0 {
		cfg.OTPResend = 60 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	priv, pub := signer.Keys()
	s := &Service{DB: d, Jobs: j, Storage: store, Hotel: hotelSvc, Profile: prof, Log: log, Cfg: cfg,
		signer: newCustomerSigner(priv, pub), ipLimiter: newIPLimiter(20, time.Hour), nowFn: time.Now, uploadTTL: 15 * time.Minute, downloadTT: time.Hour}
	s.SMS = MockSMS{Log: log}
	s.Pusher = nil // diisi VAPIDPusher oleh app bila kunci tersedia; nil = notifikasi inbox saja
	if hotelSvc != nil {
		hotelSvc.RegisterReservationHook(s.onReservationTransition)
	}
	return s
}

func (s *Service) now() time.Time { return s.nowFn().UTC() }

// devMode: kode OTP dikembalikan di respons (BV_ENV local/test) — provider SMS masih mock (D3).
func (s *Service) devMode() bool {
	return s.Cfg.Env == "local" || s.Cfg.Env == "test" || s.Cfg.Env == ""
}

// ---------- organization (endpoint publik tanpa token; org dari organization_slug) ----------

type orgCtxKey struct{}

// WithOrgID menyimpan organization aktif untuk endpoint publik.
func WithOrgID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, orgCtxKey{}, id)
}

// OrgID: organization aktif — dari principal customer/staf, atau dari resolusi slug (publik).
func OrgID(ctx context.Context) (uuid.UUID, bool) {
	if p, ok := authctx.From(ctx); ok {
		return p.OrganizationID, true
	}
	id, ok := ctx.Value(orgCtxKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

func mustOrg(ctx context.Context) uuid.UUID {
	id, ok := OrgID(ctx)
	if !ok {
		panic("bvrooms: organization missing from context")
	}
	return id
}

// ResolveOrg: slug → organization id (cache 60 detik).
func (s *Service) ResolveOrg(ctx context.Context, slug string) (uuid.UUID, string, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return uuid.Nil, "", apperr.Validation("organization_slug wajib")
	}
	if v, ok := s.orgCache.Load(slug); ok {
		e := v.(orgEntry)
		if time.Since(e.loaded) < time.Minute {
			return e.id, e.name, nil
		}
	}
	var id uuid.UUID
	var name string
	var active bool
	if err := s.DB.Pool.QueryRow(ctx, `SELECT id, name, is_active FROM organizations WHERE slug = $1`, slug).Scan(&id, &name, &active); err != nil || !active {
		return uuid.Nil, "", apperr.NotFound("Organization")
	}
	s.orgCache.Store(slug, orgEntry{id: id, name: name, loaded: time.Now()})
	return id, name, nil
}

// ---------- principal customer ----------

// customer: principal dari token customer (UserID = bvrooms_customers.id, Source = bvrooms).
func customer(ctx context.Context) (*authctx.Principal, error) {
	p, ok := authctx.From(ctx)
	if !ok || p.Source != authctx.SourceBVRooms || p.UserID == uuid.Nil {
		return nil, apperr.Unauthorized("token customer BVRooms diperlukan")
	}
	return p, nil
}

// staff: principal staf dashboard (bukan customer, bukan tenant).
func staff(ctx context.Context) (*authctx.Principal, error) {
	p, ok := authctx.From(ctx)
	if !ok || p.Source == authctx.SourceBVRooms || p.IsTenant {
		return nil, apperr.Forbidden("")
	}
	return p, nil
}

// ---------- helper umum ----------

var phoneDigits = regexp.MustCompile(`^[0-9]{8,15}$`)
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// NormalizePhone: "0812…" / "62812…" / "+62 812-…" → "+62812…" (E.164, default kode negara 62).
func NormalizePhone(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	v = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "").Replace(v)
	plus := strings.HasPrefix(v, "+")
	v = strings.TrimPrefix(v, "+")
	switch {
	case strings.HasPrefix(v, "0"):
		v = "62" + v[1:]
	case plus:
	case strings.HasPrefix(v, "62"):
	default:
		v = "62" + v
	}
	if !phoneDigits.MatchString(v) {
		return "", apperr.Validation("nomor telepon tidak valid").WithField("phone", "tidak valid")
	}
	return "+" + v, nil
}

// MaskPhone: +6281210201002 → +62812•••1002 (Figma menampilkan nomor tujuan OTP).
func MaskPhone(p string) string {
	if len(p) < 8 {
		return p
	}
	return p[:6] + strings.Repeat("•", len(p)-10) + p[len(p)-4:]
}

func hashCode(id uuid.UUID, code string) string {
	sum := sha256.Sum256([]byte(id.String() + ":" + code))
	return hex.EncodeToString(sum[:])
}

func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = '0' + b[i]%10
	}
	return string(out), nil
}

const bookingAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ0123456789" // Crockford tanpa I/L/O/U (§6)

func newBookingCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 12)
	for i := range b {
		out[i] = bookingAlphabet[int(b[i])%len(bookingAlphabet)]
	}
	return string(out), nil
}

func parseDate(v string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, apperr.Validation("tanggal harus YYYY-MM-DD")
	}
	return t, nil
}

// haversineKm: jarak dua koordinat (km) untuk "4,6 km dari Anda" & sort lokasi terdekat.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// ratingLabel (server, §5.2): konsisten di seluruh client.
func ratingLabel(avg float64, count int) string {
	switch {
	case count == 0:
		return "No Review Yet"
	case avg < 2.5:
		return "Bad"
	case avg < 4.0:
		return "Good"
	case avg < 4.8:
		return "Very Good"
	default:
		return "Awesome"
	}
}

func (s *Service) photoURL(ctx context.Context, key *string) *string {
	if key == nil || *key == "" || s.Storage == nil {
		return nil
	}
	u, err := s.Storage.PresignGet(ctx, *key, s.downloadTT)
	if err != nil {
		return nil
	}
	return &u
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(v string) *string { return &v }

// ---------- rate limiter per IP (OTP) ----------

type ipLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	max  int
	win  time.Duration
}

func newIPLimiter(max int, win time.Duration) *ipLimiter {
	return &ipLimiter{hits: map[string][]time.Time{}, max: max, win: win}
}

func (l *ipLimiter) Allow(ip string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < l.win {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	return true
}

// SetIPRateLimit: 0 = nonaktif (test).
func (s *Service) SetIPRateLimit(max int) { s.ipLimiter = newIPLimiter(max, time.Hour) }

// ---------- SMS provider (D3: vendor di-hold) ----------

// SMSProvider mengirim OTP; adapter vendor (Twilio/Zenziva/WhatsApp) menyusul tanpa mengubah kontrak.
type SMSProvider interface {
	Code() string
	SendOTP(ctx context.Context, phoneE164, code string, ttl time.Duration) error
}

// MockSMS: menulis kode ke log (dev/test).
type MockSMS struct{ Log *slog.Logger }

func (MockSMS) Code() string { return "mock" }
func (m MockSMS) SendOTP(_ context.Context, phone, code string, ttl time.Duration) error {
	if m.Log != nil {
		m.Log.Info("bvrooms otp (mock sms)", "phone", MaskPhone(phone), "code", code, "ttl", ttl.String())
	}
	return nil
}

// ---------- Web Push (VAPID) ----------

// PushSubscription: langganan Web Push customer.
type PushSubscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Pusher mengirim payload ke satu subscription; implementasi VAPID ditambahkan saat kunci tersedia.
type Pusher interface {
	Send(ctx context.Context, sub PushSubscription, payload []byte) error
}

type LogPusher struct{ Log *slog.Logger }

func (p LogPusher) Send(_ context.Context, sub PushSubscription, payload []byte) error {
	if p.Log != nil {
		p.Log.Debug("bvrooms web push (log only)", "endpoint", sub.Endpoint, "bytes", len(payload))
	}
	return nil
}

// ---------- akses listing ----------

type listingRow struct {
	PropertyID        uuid.UUID
	Listed            bool
	Slug              string
	Category          string
	DisplayName       string
	Tagline           *string
	AddressLine       *string
	District          *string
	City              *string
	Lat, Lng          *float64
	Phone, WhatsApp   *string
	CheckInTime       string
	CheckOutTime      string
	Description       []byte
	Facilities        []string
	Policies          []string
	CancellationMD    *string
	CancellationRules []byte
	PaymentWindowH    int
	BankAccounts      []byte
	AllowPayAtProp    bool
	Popularity        float64
	RatingAvg         float64
	RatingCount       int
	RatingHist        []int32
	MinRateCache      *int64
	Timezone          string
	Version           int
}

const listingSelect = `SELECT l.property_id, l.bvrooms_listed, l.slug, l.listing_category, l.display_name, l.tagline, l.address_line, l.district, l.city, l.lat, l.lng, l.phone, l.whatsapp,
	to_char(l.check_in_time, 'HH24:MI'), to_char(l.check_out_time, 'HH24:MI'), l.description_sections, l.facilities, l.policies, l.cancellation_policy_md, l.cancellation_rules,
	l.payment_window_hours, l.bank_accounts, l.allow_pay_at_property, l.popularity_score, l.rating_avg, l.rating_count, l.rating_hist, l.min_rate_cache, p.timezone, l.version
	FROM bvrooms_property_listings l JOIN properties p ON p.location_id = l.property_id JOIN locations loc ON loc.id = l.property_id`

func scanListing(row pgx.Row) (*listingRow, error) {
	var r listingRow
	if err := row.Scan(&r.PropertyID, &r.Listed, &r.Slug, &r.Category, &r.DisplayName, &r.Tagline, &r.AddressLine, &r.District, &r.City, &r.Lat, &r.Lng, &r.Phone, &r.WhatsApp,
		&r.CheckInTime, &r.CheckOutTime, &r.Description, &r.Facilities, &r.Policies, &r.CancellationMD, &r.CancellationRules,
		&r.PaymentWindowH, &r.BankAccounts, &r.AllowPayAtProp, &r.Popularity, &r.RatingAvg, &r.RatingCount, &r.RatingHist, &r.MinRateCache, &r.Timezone, &r.Version); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Service) listingBySlugTx(ctx context.Context, tx pgx.Tx, slug string, publicOnly bool) (*listingRow, error) {
	r, err := scanListing(tx.QueryRow(ctx, listingSelect+` WHERE l.slug = $1 AND loc.deleted_at IS NULL AND p.status = 'active'`, strings.ToLower(strings.TrimSpace(slug))))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Property")
		}
		return nil, err
	}
	if publicOnly && !r.Listed {
		return nil, apperr.NotFound("Property")
	}
	return r, nil
}

func (s *Service) listingByIDTx(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID) (*listingRow, error) {
	r, err := scanListing(tx.QueryRow(ctx, listingSelect+` WHERE l.property_id = $1`, propertyID))
	if err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Listing")
		}
		return nil, err
	}
	return r, nil
}

func (r *listingRow) location() *time.Location {
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}

func (r *listingRow) terminology() map[string]string {
	if r.Category == "apartment" {
		return map[string]string{"unit_label": "Unit", "guest_label": "Penghuni", "type_label": "Tipe Unit"}
	}
	return map[string]string{"unit_label": "Kamar", "guest_label": "Tamu", "type_label": "Tipe Kamar"}
}
