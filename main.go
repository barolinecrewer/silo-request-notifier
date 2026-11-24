package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
)

//go:embed manifest.json
var manifest []byte

var (
	topicPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	ntfyTokenPattern = regexp.MustCompile(`^tk_[A-Za-z0-9_-]{1,128}$`)
	webhookPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type deliveryConfig struct {
	serverURL, topic, ntfyToken, webhookToken string
}

type receiver struct {
	pluginv1.UnimplementedHttpRoutesServer
	config atomic.Pointer[deliveryConfig]
	client *http.Client
}

type webhookEvent struct {
	Version int    `json:"version"`
	Event   string `json:"event"`
	Test    bool   `json:"test"`
	Request struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Year      int    `json:"year"`
		Requester string `json:"requester_name"`
	} `json:"request"`
}

func (r *receiver) configure(_ context.Context, entries []*pluginv1.ConfigEntry) error {
	for _, entry := range entries {
		if entry.GetKey() != "delivery" {
			continue
		}
		fields := entry.GetValue().GetFields()
		cfg := &deliveryConfig{
			serverURL:    strings.TrimRight(strings.TrimSpace(fields["server_url"].GetStringValue()), "/") + "/",
			topic:        fields["topic"].GetStringValue(),
			ntfyToken:    fields["ntfy_token"].GetStringValue(),
			webhookToken: fields["webhook_token"].GetStringValue(),
		}
		endpoint, err := url.Parse(cfg.serverURL)
		if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil ||
			endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
			return fmt.Errorf("enter an HTTPS ntfy server URL without credentials, a query, or a fragment")
		}
		if !topicPattern.MatchString(cfg.topic) || !ntfyTokenPattern.MatchString(cfg.ntfyToken) || !webhookPattern.MatchString(cfg.webhookToken) {
			return fmt.Errorf("enter an ntfy topic, an ntfy access token, and a webhook token generated with openssl rand -hex 32")
		}
		r.config.Store(cfg)
		return nil
	}
	r.config.Store(nil)
	return nil
}

func (r *receiver) Handle(ctx context.Context, req *pluginv1.HandleHTTPRequest) (*pluginv1.HandleHTTPResponse, error) {
	status := r.receive(ctx, req)
	return &pluginv1.HandleHTTPResponse{StatusCode: int32(status)}, nil
}

func (r *receiver) receive(ctx context.Context, req *pluginv1.HandleHTTPRequest) int {
	cfg := r.config.Load()
	token := req.GetQuery().GetFields()["token"].GetStringValue()
	// Silo strips webhook signature headers from plugin HTTP routes.
	if req.GetMethod() != http.MethodPost || req.GetPath() != "/requests" || cfg == nil ||
		subtle.ConstantTimeCompare([]byte(token), []byte(cfg.webhookToken)) != 1 {
		return http.StatusNotFound
	}

	var event webhookEvent
	if len(req.GetBody()) > 64<<10 || json.Unmarshal(req.GetBody(), &event) != nil || event.Version != 1 {
		return http.StatusBadRequest
	}
	var title, message string
	switch {
	case event.Test && event.Event == "content.added":
		title, message = "Silo request notifier test", "Silo can deliver request alerts to this ntfy topic."
	case !event.Test && event.Event == "request.submitted":
		message = strings.TrimSpace(event.Request.Title)
		if event.Request.ID == "" || message == "" {
			return http.StatusBadRequest
		}
		title = "New Silo media request"
		if event.Request.Year > 0 {
			message += fmt.Sprintf(" (%d)", event.Request.Year)
		}
		if requester := strings.TrimSpace(event.Request.Requester); requester != "" {
			message += "\nRequested by " + requester
		}
	default:
		return http.StatusNoContent
	}

	payload, err := json.Marshal(map[string]string{"topic": cfg.topic, "title": title, "message": message})
	if err != nil {
		return http.StatusInternalServerError
	}
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.serverURL, bytes.NewReader(payload))
	if err != nil {
		return http.StatusInternalServerError
	}
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Authorization", "Bearer "+cfg.ntfyToken)
	resp, err := r.client.Do(out)
	if err != nil {
		return http.StatusBadGateway
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return http.StatusBadGateway
	}
	return http.StatusNoContent
}

func main() {
	r := &receiver{
		client: &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	runtime.ServeManifestWithOptions(manifest, "", runtime.CapabilityServers{HttpRoutes: r}, runtime.WithConfigure(r.configure))
}
