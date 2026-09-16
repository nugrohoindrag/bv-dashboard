package bvrooms

import (
	"context"
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// VAPIDPusher: Web Push (RFC 8030/8291) untuk PWA BVRooms. Aktif bila BV_VAPID_PUBLIC_KEY & BV_VAPID_PRIVATE_KEY diisi;
// kunci dibuat dengan `bvctl vapid-keygen`. Public key yang sama dipakai client saat `pushManager.subscribe`.
type VAPIDPusher struct {
	PublicKey  string
	PrivateKey string
	Subscriber string // mailto:ops@buildingvision.id
	Timeout    time.Duration
}

func (p VAPIDPusher) Send(ctx context.Context, sub PushSubscription, payload []byte) error {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{Subscriber: p.Subscriber, VAPIDPublicKey: p.PublicKey, VAPIDPrivateKey: p.PrivateKey, TTL: 24 * 3600, Urgency: webpush.UrgencyNormal})
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

// ErrSubscriptionGone: endpoint sudah tidak berlaku (410/404) → subscription direvoke.
var ErrSubscriptionGone = fmt.Errorf("web push subscription gone")

// GenerateVAPIDKeys: pasangan kunci untuk konfigurasi (bvctl).
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}
