package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// FCMPusher: Firebase Cloud Messaging HTTP v1 (TAD §5.14) via service account (golang.org/x/oauth2/google).
type FCMPusher struct {
	projectID string
	client    *http.Client
}

// NewFCM: serviceAccount = path file JSON atau JSON inline.
func NewFCM(ctx context.Context, projectID, serviceAccount string) (*FCMPusher, error) {
	if projectID == "" || serviceAccount == "" {
		return nil, fmt.Errorf("fcm: project id & service account wajib")
	}
	raw := []byte(serviceAccount)
	if !strings.HasPrefix(strings.TrimSpace(serviceAccount), "{") {
		b, err := os.ReadFile(serviceAccount)
		if err != nil {
			return nil, fmt.Errorf("fcm: read service account: %w", err)
		}
		raw = b
	}
	creds, err := google.CredentialsFromJSON(ctx, raw, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("fcm: credentials: %w", err)
	}
	client := oauth2.NewClient(ctx, creds.TokenSource)
	client.Timeout = 10 * time.Second
	return &FCMPusher{projectID: projectID, client: client}, nil
}

func (f *FCMPusher) Send(ctx context.Context, token string, notificationID uuid.UUID, deepLink, title, body string) error {
	payload := map[string]any{
		"message": map[string]any{
			"token":        token,
			"notification": map[string]string{"title": title, "body": firstLine(body)},
			"data":         map[string]string{"notification_id": notificationID.String(), "deep_link": deepLink},
			"android":      map[string]any{"priority": "high", "notification": map[string]string{"channel_id": "buildingvision_ops", "click_action": "FLUTTER_NOTIFICATION_CLICK"}},
			"apns":         map[string]any{"payload": map[string]any{"aps": map[string]any{"sound": "default"}}},
		},
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/v1/projects/"+f.projectID+"/messages:send", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 || strings.Contains(string(rb), "UNREGISTERED") || strings.Contains(string(rb), "INVALID_ARGUMENT") {
		return InvalidToken(fmt.Errorf("fcm %d: %s", resp.StatusCode, string(rb)))
	}
	return fmt.Errorf("fcm %d: %s", resp.StatusCode, string(rb))
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}
