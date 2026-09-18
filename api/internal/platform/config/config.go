// Package config memuat konfigurasi 12-factor dari environment (TAD §1.1, §11).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env               string // local | staging | production
	HTTPAddr          string
	PublicURL         string // https://app.buildingvision.id
	DatabaseURL       string
	WorkerDatabaseURL string

	// Auth
	JWTPrivateKeyPEM string // Ed25519 PKCS8 PEM; kosong = generate ephemeral (local only)
	JWTPublicKeyPEM  string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	CookieDomain     string
	CookieSecure     bool

	// Object storage (S3 compatible)
	S3Endpoint         string
	S3PublicEndpoint   string // endpoint yang dapat diakses browser/app untuk presigned URL (mis. https://app.example.com); kosong = S3Endpoint
	OTPStaticCode      string // BVRooms: kode OTP tetap (demo/pilot tanpa vendor SMS); kosong = acak 4 digit
	OTPExposeCode      bool   // BVRooms: kembalikan dev_code di respons OTP walau production (provider mock)
	BVRoomsAuthMethod  string // BVRooms: pin (default, tanpa SMS) | otp
	BVRoomsDefaultPIN  string // BVRooms: PIN default akun yang belum mengatur PIN (bawaan 1234)
	S3Region           string
	S3Bucket           string
	S3AccessKey        string
	S3SecretKey        string
	S3UsePathStyle     bool
	PresignUploadTTL   time.Duration
	PresignDownloadTTL time.Duration
	MaxUploadBytes     int64
	MaxImageBytes      int64 // batas foto (image/*) semua kanal; dokumen/PDF memakai MaxUploadBytes

	// Push
	FCMProjectID          string
	FCMServiceAccountJSON string // path atau inline JSON
	// BVRooms Web Push (VAPID) — kosong = push dinonaktifkan; kunci dari `bvctl vapid-keygen`
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// Demo Seed Database (§39): tooling demo aktif di local/staging/test; production butuh BV_DEMO_ENABLED=true
	DemoEnabled bool

	// Misc
	LogLevel            string
	CORSOrigins         []string
	OverviewCacheTTL    time.Duration
	DueSoonWindow       time.Duration
	MinMobileAppVersion string
	QRBaseURL           string
	MetricsAddr         string // alamat internal /metrics (kosong = nonaktif)

	// Website, self-serve onboarding & free trial (Website PRD v1.1)
	WebsiteURL          string        // https://buildingvision.id (public website; CTA Login/Start Free Trial → PublicURL)
	TrialDays           int           // §24: 14 hari
	TrialEndingSoonDays int           // §31: trial_ending_soon bila sisa ≤ N hari
	SignupTokenTTL      time.Duration // masa berlaku link verifikasi email
	SalesEmail          string        // penerima Book a Demo (§34)
	GrowthEventsEnabled bool          // §40 analytics funnel (server-side store)

	// Email (SMTP; kosong = mailer log-only, dev memakai Mailpit :1025)
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	SMTPStartTLS bool
}

func Load() (Config, error) {
	c := Config{
		Env:                   getenv("BV_ENV", "local"),
		HTTPAddr:              getenv("BV_HTTP_ADDR", ":8080"),
		PublicURL:             getenv("BV_PUBLIC_URL", "http://localhost:5173"),
		DatabaseURL:           getenv("BV_DATABASE_URL", "postgres://bv_app:bv_app_dev@localhost:5432/buildingvision?sslmode=disable"),
		WorkerDatabaseURL:     getenv("BV_WORKER_DATABASE_URL", ""),
		JWTPrivateKeyPEM:      getenv("BV_JWT_PRIVATE_KEY", ""),
		JWTPublicKeyPEM:       getenv("BV_JWT_PUBLIC_KEY", ""),
		AccessTokenTTL:        getdur("BV_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:       getdur("BV_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		CookieDomain:          getenv("BV_COOKIE_DOMAIN", ""),
		CookieSecure:          getbool("BV_COOKIE_SECURE", false),
		S3Endpoint:            getenv("BV_S3_ENDPOINT", "http://localhost:9000"),
		S3PublicEndpoint:      getenv("BV_S3_PUBLIC_ENDPOINT", ""),
		OTPStaticCode:         getenv("BV_OTP_STATIC_CODE", ""),
		OTPExposeCode:         getenv("BV_OTP_EXPOSE_CODE", "") == "true",
		BVRoomsAuthMethod:     getenv("BV_BVROOMS_AUTH", "pin"),
		BVRoomsDefaultPIN:     getenv("BV_BVROOMS_DEFAULT_PIN", "1234"),
		S3Region:              getenv("BV_S3_REGION", "ap-southeast-3"),
		S3Bucket:              getenv("BV_S3_BUCKET", "buildingvision"),
		S3AccessKey:           getenv("BV_S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:           getenv("BV_S3_SECRET_KEY", "minioadmin"),
		S3UsePathStyle:        getbool("BV_S3_PATH_STYLE", true),
		PresignUploadTTL:      getdur("BV_PRESIGN_UPLOAD_TTL", 15*time.Minute),
		PresignDownloadTTL:    getdur("BV_PRESIGN_DOWNLOAD_TTL", 10*time.Minute),
		MaxUploadBytes:        getint64("BV_MAX_UPLOAD_BYTES", 10*1024*1024),
		MaxImageBytes:         getint64("BV_MAX_IMAGE_BYTES", 500*1024),
		FCMProjectID:          getenv("BV_FCM_PROJECT_ID", ""),
		FCMServiceAccountJSON: getenv("BV_FCM_SERVICE_ACCOUNT", ""),
		VAPIDPublicKey:        getenv("BV_VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:       getenv("BV_VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:          getenv("BV_VAPID_SUBJECT", "mailto:ops@buildingvision.id"),
		DemoEnabled:           getenv("BV_DEMO_ENABLED", "") == "true",
		LogLevel:              getenv("BV_LOG_LEVEL", "info"),
		CORSOrigins:           splitCSV(getenv("BV_CORS_ORIGINS", "http://localhost:5173")),
		OverviewCacheTTL:      getdur("BV_OVERVIEW_CACHE_TTL", 30*time.Second),
		DueSoonWindow:         getdur("BV_DUE_SOON_WINDOW", 60*time.Minute),
		MinMobileAppVersion:   getenv("BV_MIN_MOBILE_APP_VERSION", "0.1.0"),
		QRBaseURL:             getenv("BV_QR_BASE_URL", "https://bv.link/q/"),
		MetricsAddr:           getenv("BV_METRICS_ADDR", ""),
		WebsiteURL:            getenv("BV_WEBSITE_URL", "http://localhost:5175"),
		TrialDays:             int(getint64("BV_TRIAL_DAYS", 14)),
		TrialEndingSoonDays:   int(getint64("BV_TRIAL_ENDING_SOON_DAYS", 3)),
		SignupTokenTTL:        getdur("BV_SIGNUP_TOKEN_TTL", 24*time.Hour),
		SalesEmail:            getenv("BV_SALES_EMAIL", "sales@buildingvision.id"),
		GrowthEventsEnabled:   getbool("BV_GROWTH_EVENTS", true),
		SMTPHost:              getenv("BV_SMTP_HOST", ""),
		SMTPPort:              int(getint64("BV_SMTP_PORT", 1025)),
		SMTPUser:              getenv("BV_SMTP_USER", ""),
		SMTPPassword:          getenv("BV_SMTP_PASSWORD", ""),
		SMTPFrom:              getenv("BV_SMTP_FROM", "BuildingVision <no-reply@buildingvision.id>"),
		SMTPStartTLS:          getbool("BV_SMTP_STARTTLS", false),
	}
	if c.WorkerDatabaseURL == "" {
		c.WorkerDatabaseURL = c.DatabaseURL
	}
	if c.Env == "production" && c.JWTPrivateKeyPEM == "" {
		return c, fmt.Errorf("BV_JWT_PRIVATE_KEY wajib di production")
	}
	return c, nil
}

func (c Config) IsLocal() bool { return c.Env == "local" }

func getenv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}
func getdur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
func getbool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
func getint64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
