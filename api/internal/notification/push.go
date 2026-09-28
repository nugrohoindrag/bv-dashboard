package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/buildingvision/api/internal/platform/authctx"
)

// ---------- Push per platform (PRD P3 v2.1 §6.2, keputusan D-P3-04: Web Push PWA + FCM APK, satu rule & preferensi) ----------
//
// device_tokens.push_kind menentukan adapter: `fcm` (APK Staff/Tenant, token FCM) atau `webpush` (PWA, token = endpoint
// subscription + kunci p256dh/auth). Payload push hanya ringkasan + deep link; isi lengkap tetap di Inbox (TAD §5.14).

// PushMessage: isi push untuk satu perangkat.
type PushMessage struct {
	NotificationID uuid.UUID
	App            string // staff | tenant — FCM memakai channel Android berbeda
	Title          string
	Body           string
	DeepLink       string
	Severity       string
}

// Pusher: adapter FCM (HTTP v1). Nil = tidak ada push FCM.
type Pusher interface {
	Send(ctx context.Context, token string, msg PushMessage) error
}

// WebPusher: adapter Web Push (RFC 8030/8291, VAPID). Nil = tidak ada Web Push.
type WebPusher interface {
	SendWebPush(ctx context.Context, endpoint, p256dh, auth string, payload []byte) error
}

// ErrSubscriptionGone: endpoint Web Push sudah tidak berlaku (404/410) → perangkat dihapus.
var ErrSubscriptionGone = errors.New("web push subscription gone")

// VAPIDSender: Web Push dengan kunci VAPID (`bvctl vapid-keygen`, BV_VAPID_*) — kunci yang sama dengan BVRooms.
type VAPIDSender struct {
	PublicKey  string
	PrivateKey string
	Subscriber string
	Timeout    time.Duration
}

func (v VAPIDSender) SendWebPush(ctx context.Context, endpoint, p256dh, auth string, payload []byte) error {
	timeout := v.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: endpoint, Keys: webpush.Keys{P256dh: p256dh, Auth: auth}},
		&webpush.Options{Subscriber: v.Subscriber, VAPIDPublicKey: v.PublicKey, VAPIDPrivateKey: v.PrivateKey, TTL: 24 * 3600, Urgency: webpush.UrgencyHigh})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
		return ErrSubscriptionGone
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("web push: status %d", resp.StatusCode)
	}
	return nil
}

// WebPushPayload: JSON yang diterima service worker Tenant PWA (event `push`).
type WebPushPayload struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	DeepLink       string    `json:"deep_link"`
	Severity       string    `json:"severity"`
	Tag            string    `json:"tag"`
}

// PushConfig: konfigurasi klien untuk registrasi push (GET /push/config).
type PushConfig struct {
	WebPush struct {
		Enabled   bool   `json:"enabled"`
		PublicKey string `json:"public_key,omitempty"`
	} `json:"web_push"`
	FCM struct {
		Enabled bool `json:"enabled"`
	} `json:"fcm"`
	Registered []RegisteredDevice `json:"devices"`
}

type RegisteredDevice struct {
	Platform   string    `json:"platform"`
	PushKind   string    `json:"push_kind"`
	App        string    `json:"app"`
	LastSeenAt time.Time `json:"last_seen_at"`
	// Endpoint/Token dipotong agar tidak bocor penuh
	TokenHint string `json:"token_hint"`
}

func (s *Service) PushConfig(ctx context.Context) (*PushConfig, error) {
	p := authctx.Must(ctx)
	out := &PushConfig{Registered: []RegisteredDevice{}}
	out.WebPush.Enabled = s.VAPIDPublicKey != ""
	out.WebPush.PublicKey = s.VAPIDPublicKey
	out.FCM.Enabled = s.FCMEnabled
	err := s.DB.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT platform, push_kind, app, last_seen_at, token FROM device_tokens WHERE user_id = $1 ORDER BY last_seen_at DESC LIMIT 20`, p.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d RegisteredDevice
			var tok string
			if err := rows.Scan(&d.Platform, &d.PushKind, &d.App, &d.LastSeenAt, &tok); err != nil {
				return err
			}
			if len(tok) > 12 {
				tok = "…" + tok[len(tok)-12:]
			}
			d.TokenHint = tok
			out.Registered = append(out.Registered, d)
		}
		return rows.Err()
	})
	return out, err
}

// SendPush (worker): kirim notifikasi ke seluruh perangkat user sesuai jenisnya; token/subscription invalid → hapus perangkat.
func (s *Service) SendPush(ctx context.Context, orgID, notificationID uuid.UUID) error {
	if s.Pusher == nil && s.WebPush == nil {
		return nil
	}
	return s.DB.WithOrgTx(ctx, orgID, func(ctx context.Context, tx pgx.Tx) error {
		var userID uuid.UUID
		var title, body, severity string
		var deepLink, objType *string
		var objID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT user_id, title, body, deep_link, severity, object_type, object_id FROM notifications WHERE id = $1`, notificationID).Scan(&userID, &title, &body, &deepLink, &severity, &objType, &objID); err != nil {
			return nil
		}
		rows, err := tx.Query(ctx, `SELECT id, token, push_kind, COALESCE(webpush_p256dh,''), COALESCE(webpush_auth,''), app FROM device_tokens WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		type dt struct {
			id                      uuid.UUID
			token, kind, p256, auth string
			app                     string
		}
		var devices []dt
		for rows.Next() {
			var d dt
			if rows.Scan(&d.id, &d.token, &d.kind, &d.p256, &d.auth, &d.app) == nil {
				devices = append(devices, d)
			}
		}
		rows.Close()
		msg := PushMessage{NotificationID: notificationID, Title: title, Body: body, DeepLink: derefStr(deepLink), Severity: severity}
		var lastErr error
		delivered, attempted := false, false
		for _, d := range devices {
			msg.App = d.app
			var err error
			switch d.kind {
			case "webpush":
				if s.WebPush == nil || d.p256 == "" || d.auth == "" {
					continue
				}
				// tag per object: notifikasi object yang sama saling menggantikan, object berbeda tidak saling menimpa
				tag := notificationID.String()
				if objType != nil && objID != nil {
					tag = *objType + ":" + objID.String()
				}
				payload, _ := json.Marshal(WebPushPayload{NotificationID: notificationID, Title: title, Body: firstLine(body), DeepLink: msg.DeepLink, Severity: severity, Tag: tag})
				attempted = true
				err = s.WebPush.SendWebPush(ctx, d.token, d.p256, d.auth, payload)
				if errors.Is(err, ErrSubscriptionGone) {
					_, _ = tx.Exec(ctx, `DELETE FROM device_tokens WHERE id = $1`, d.id)
				}
			default:
				if s.Pusher == nil {
					continue
				}
				attempted = true
				err = s.Pusher.Send(ctx, d.token, msg)
				if IsInvalidToken(err) {
					_, _ = tx.Exec(ctx, `DELETE FROM device_tokens WHERE id = $1`, d.id)
				}
			}
			if err != nil {
				lastErr = err
				continue
			}
			delivered = true
		}
		if attempted {
			_, _ = tx.Exec(ctx, `UPDATE notifications SET push_attempted_at = now() WHERE id = $1`, notificationID)
		}
		if delivered {
			_, _ = tx.Exec(ctx, `UPDATE notifications SET delivered_push_at = now(), push_error = NULL WHERE id = $1`, notificationID)
		} else if lastErr != nil {
			msg := lastErr.Error()
			if len(msg) > 300 {
				msg = msg[:300]
			}
			_, _ = tx.Exec(ctx, `UPDATE notifications SET push_error = $2 WHERE id = $1`, notificationID, msg)
		} else if attempted {
			_, _ = tx.Exec(ctx, `UPDATE notifications SET push_error = 'no_device' WHERE id = $1`, notificationID)
		}
		return nil
	})
}

// firstLine dipakai FCM & Web Push (body notifikasi push satu baris).
func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}
