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
	S3Region           string
	S3Bucket           string
	S3AccessKey        string
	S3SecretKey        string
	S3UsePathStyle     bool
	PresignUploadTTL   time.Duration
	PresignDownloadTTL time.Duration
	MaxUploadBytes     int64

	// Push
	FCMProjectID          string
	FCMServiceAccountJSON string // path atau inline JSON

	// Misc
	LogLevel            string
	CORSOrigins         []string
	OverviewCacheTTL    time.Duration
	DueSoonWindow       time.Duration
	MinMobileAppVersion string
	QRBaseURL           string
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
		S3Region:              getenv("BV_S3_REGION", "ap-southeast-3"),
		S3Bucket:              getenv("BV_S3_BUCKET", "buildingvision"),
		S3AccessKey:           getenv("BV_S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:           getenv("BV_S3_SECRET_KEY", "minioadmin"),
		S3UsePathStyle:        getbool("BV_S3_PATH_STYLE", true),
		PresignUploadTTL:      getdur("BV_PRESIGN_UPLOAD_TTL", 15*time.Minute),
		PresignDownloadTTL:    getdur("BV_PRESIGN_DOWNLOAD_TTL", 10*time.Minute),
		MaxUploadBytes:        getint64("BV_MAX_UPLOAD_BYTES", 10*1024*1024),
		FCMProjectID:          getenv("BV_FCM_PROJECT_ID", ""),
		FCMServiceAccountJSON: getenv("BV_FCM_SERVICE_ACCOUNT", ""),
		LogLevel:              getenv("BV_LOG_LEVEL", "info"),
		CORSOrigins:           splitCSV(getenv("BV_CORS_ORIGINS", "http://localhost:5173")),
		OverviewCacheTTL:      getdur("BV_OVERVIEW_CACHE_TTL", 30*time.Second),
		DueSoonWindow:         getdur("BV_DUE_SOON_WINDOW", 60*time.Minute),
		MinMobileAppVersion:   getenv("BV_MIN_MOBILE_APP_VERSION", "0.1.0"),
		QRBaseURL:             getenv("BV_QR_BASE_URL", "https://bv.link/q/"),
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
