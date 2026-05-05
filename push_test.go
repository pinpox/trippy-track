package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupPushTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	db, err := initDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	var userID int
	err = db.QueryRow(
		"INSERT INTO users (oidc_subject, email, name) VALUES (?, ?, ?) RETURNING id",
		"test-sub", "test@example.com", "Test User",
	).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	trip, err := createTrip(db, "Test Trip", userID)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		VAPID: VAPIDConfig{
			PublicKey:  "BDtest-public-key",
			PrivateKey: "test-private-key",
			Contact:    "mailto:test@example.com",
		},
	}

	srv, err := newServer(db, ":0", t.TempDir(), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}

	return srv, trip.ViewToken
}

func TestVAPIDPublicKeyEndpoint(t *testing.T) {
	srv, viewToken := setupPushTestServer(t)
	mux := srv.routes()

	t.Run("returns public key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/push/vapid-public-key", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if body["publicKey"] != "BDtest-public-key" {
			t.Fatalf("expected BDtest-public-key, got %s", body["publicKey"])
		}
	})

	t.Run("returns 404 when VAPID not configured", func(t *testing.T) {
		db, err := initDB(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		srvNoPush, err := newServer(db, ":0", t.TempDir(), nil, Config{})
		if err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("GET", "/t/"+viewToken+"/push/vapid-public-key", nil)
		w := httptest.NewRecorder()
		srvNoPush.routes().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestPushSubscribeEndpoint(t *testing.T) {
	srv, viewToken := setupPushTestServer(t)
	mux := srv.routes()

	t.Run("successful subscribe", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/abc","keys":{"p256dh":"testkey123","auth":"testauthkey"}}`
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify stored in DB
		trip, _ := getTripByViewToken(srv.db, viewToken)
		subs, err := getPushSubscriptionsForTrip(srv.db, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 {
			t.Fatalf("expected 1 subscription, got %d", len(subs))
		}
		if subs[0].Endpoint != "https://fcm.example.com/push/abc" {
			t.Fatalf("wrong endpoint: %s", subs[0].Endpoint)
		}
		if subs[0].KeyP256dh != "testkey123" {
			t.Fatalf("wrong p256dh key: %s", subs[0].KeyP256dh)
		}
		if subs[0].KeyAuth != "testauthkey" {
			t.Fatalf("wrong auth key: %s", subs[0].KeyAuth)
		}
	})

	t.Run("re-subscribe replaces existing", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/abc","keys":{"p256dh":"newkey","auth":"newauth"}}`
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}

		trip, _ := getTripByViewToken(srv.db, viewToken)
		subs, _ := getPushSubscriptionsForTrip(srv.db, trip.ID)
		if len(subs) != 1 {
			t.Fatalf("expected 1 subscription after re-subscribe, got %d", len(subs))
		}
		if subs[0].KeyP256dh != "newkey" {
			t.Fatalf("key not updated: %s", subs[0].KeyP256dh)
		}
	})

	t.Run("invalid token returns 404", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/xyz","keys":{"p256dh":"k","auth":"a"}}`
		req := httptest.NewRequest("POST", "/t/nonexistent/push/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/xyz"}`
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/subscribe", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestPushUnsubscribeEndpoint(t *testing.T) {
	srv, viewToken := setupPushTestServer(t)
	mux := srv.routes()

	// First subscribe
	trip, _ := getTripByViewToken(srv.db, viewToken)
	savePushSubscription(srv.db, trip.ID, "https://fcm.example.com/push/del", "k", "a")

	t.Run("successful unsubscribe", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/del"}`
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/unsubscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		subs, _ := getPushSubscriptionsForTrip(srv.db, trip.ID)
		if len(subs) != 0 {
			t.Fatalf("expected 0 subscriptions after unsubscribe, got %d", len(subs))
		}
	})

	t.Run("unsubscribe nonexistent endpoint is ok", func(t *testing.T) {
		body := `{"endpoint":"https://fcm.example.com/push/doesnotexist"}`
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/unsubscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("missing endpoint returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/t/"+viewToken+"/push/unsubscribe", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestPushSubscriptionsArePerTrip(t *testing.T) {
	db, err := initDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var userID int
	db.QueryRow(
		"INSERT INTO users (oidc_subject, email, name) VALUES (?, ?, ?) RETURNING id",
		"test-sub", "test@example.com", "Test User",
	).Scan(&userID)

	trip1, _ := createTrip(db, "Trip 1", userID)
	trip2, _ := createTrip(db, "Trip 2", userID)

	savePushSubscription(db, trip1.ID, "https://push.example.com/sub1", "k1", "a1")
	savePushSubscription(db, trip1.ID, "https://push.example.com/sub2", "k2", "a2")
	savePushSubscription(db, trip2.ID, "https://push.example.com/sub3", "k3", "a3")

	subs1, _ := getPushSubscriptionsForTrip(db, trip1.ID)
	subs2, _ := getPushSubscriptionsForTrip(db, trip2.ID)

	if len(subs1) != 2 {
		t.Fatalf("trip1: expected 2 subscriptions, got %d", len(subs1))
	}
	if len(subs2) != 1 {
		t.Fatalf("trip2: expected 1 subscription, got %d", len(subs2))
	}
}

func TestPushSubscriptionCascadeDelete(t *testing.T) {
	db, err := initDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var userID int
	db.QueryRow(
		"INSERT INTO users (oidc_subject, email, name) VALUES (?, ?, ?) RETURNING id",
		"test-sub", "test@example.com", "Test User",
	).Scan(&userID)

	trip, _ := createTrip(db, "Trip", userID)
	savePushSubscription(db, trip.ID, "https://push.example.com/cascade", "k", "a")

	// Delete the trip
	db.Exec("DELETE FROM trips WHERE id = ?", trip.ID)

	subs, _ := getPushSubscriptionsForTrip(db, trip.ID)
	if len(subs) != 0 {
		t.Fatalf("expected subscriptions to be cascade-deleted, got %d", len(subs))
	}
}
