package vpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDisassociateUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mdm/v2/assets/disassociate" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		var body struct {
			Assets []map[string]string `json:"assets"`
			Users  []string            `json:"clientUserIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Assets) != 1 || body.Assets[0]["adamId"] != "123" || body.Assets[0]["pricingParam"] != "STDQ" {
			t.Errorf("assets = %+v", body.Assets)
		}
		if !reflect.DeepEqual(body.Users, []string{"user-1", "user-2"}) {
			t.Errorf("clientUserIds = %v", body.Users)
		}
		_, _ = w.Write([]byte(`{"eventId":"event-1"}`))
	}))
	defer server.Close()

	client := New(server.URL+"/mdm/v2", "token")
	client.HTTP = server.Client()
	res, err := client.DisassociateUsers(context.Background(), "123", "", []string{"user-1", "user-2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.EventID != "event-1" {
		t.Fatalf("EventID = %q", res.EventID)
	}
}
