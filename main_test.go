package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRequestWebhook(t *testing.T) {
	type publication struct {
		token string
		body  map[string]string
	}
	published := make(chan publication, 5)
	var reply atomic.Int32
	ntfy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
			t.Error("unexpected ntfy request")
		}
		var body map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		published <- publication{req.Header.Get("Authorization"), body}
		w.WriteHeader(int(reply.Load()))
	}))
	defer ntfy.Close()

	r := &receiver{client: ntfy.Client()}
	token := strings.Repeat("a", 64)
	settings, err := structpb.NewStruct(map[string]any{"server_url": ntfy.URL, "topic": "silo-requests", "ntfy_token": "tk_test", "webhook_token": token})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "delivery", Value: settings}}); err != nil {
		t.Fatal(err)
	}

	const submitted = `{"version":1,"event":"request.submitted","request":{"id":"r1","title":"Arrival","year":2016,"requester_name":"Caroline"}}`
	for _, tc := range []struct {
		name, token, body, message string
		response, want             int
	}{
		{"unauthorized", "", submitted, "", 500, 404},
		{"approval ignored", token, `{"version":1,"event":"request.approved"}`, "", 500, 204},
		{"bad payload", token, `{"version":1,"event":"request.submitted"}`, "", 500, 400},
		{"submission", token, submitted, "Arrival (2016)\nRequested by Caroline", 200, 204},
		{"test alert", token, `{"version":1,"event":"content.added","test":true}`, "Silo can deliver request alerts to this ntfy topic.", 200, 204},
		{"ntfy unavailable", token, submitted, "Arrival (2016)\nRequested by Caroline", 503, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply.Store(int32(tc.response))
			query, err := structpb.NewStruct(map[string]any{"token": tc.token})
			if err != nil {
				t.Fatal(err)
			}
			result, err := r.Handle(context.Background(), &pluginv1.HandleHTTPRequest{
				Method: "POST", Path: "/requests", Body: []byte(tc.body), Query: query,
			})
			if err != nil || int(result.GetStatusCode()) != tc.want {
				t.Fatalf("handle = %v, %v, want HTTP %d", result, err, tc.want)
			}
			select {
			case got := <-published:
				if tc.message == "" || got.token != "Bearer tk_test" || got.body["topic"] != "silo-requests" || got.body["message"] != tc.message || got.body["title"] == "" {
					t.Fatalf("unexpected ntfy publication: %#v", got)
				}
			default:
				if tc.message != "" {
					t.Fatal("no notification published")
				}
			}
		})
	}
}
