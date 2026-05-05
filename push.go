package main

import (
	"encoding/json"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func (s *Server) pushEnabled() bool {
	return s.config.VAPID.PublicKey != "" && s.config.VAPID.PrivateKey != ""
}

func (s *Server) handleVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if !s.pushEnabled() {
		http.Error(w, "push notifications not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"publicKey": s.config.VAPID.PublicKey,
	})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.pushEnabled() {
		http.Error(w, "push notifications not configured", http.StatusNotFound)
		return
	}

	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	if err := savePushSubscription(s.db, trip.ID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth); err != nil {
		log.Printf("save push subscription: %v", err)
		http.Error(w, "failed to save subscription", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Endpoint == "" {
		http.Error(w, "missing endpoint", http.StatusBadRequest)
		return
	}

	if err := deletePushSubscription(s.db, body.Endpoint); err != nil {
		log.Printf("delete push subscription: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) sendPushNotifications(tripID, tripName, entryBody, viewToken string) {
	if !s.pushEnabled() {
		return
	}

	subs, err := getPushSubscriptionsForTrip(s.db, tripID)
	if err != nil {
		log.Printf("get push subscriptions: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	notifBody := entryBody
	if len(notifBody) > 100 {
		notifBody = notifBody[:100] + "…"
	}
	if notifBody == "" {
		notifBody = "New entry posted"
	}

	payload, _ := json.Marshal(map[string]string{
		"title": tripName + " — New Update",
		"body":  notifBody,
		"url":   "/t/" + viewToken,
	})

	for _, sub := range subs {
		resp, err := webpush.SendNotification(payload, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.KeyP256dh,
				Auth:   sub.KeyAuth,
			},
		}, &webpush.Options{
			Subscriber:      s.config.VAPID.Contact,
			VAPIDPublicKey:  s.config.VAPID.PublicKey,
			VAPIDPrivateKey: s.config.VAPID.PrivateKey,
			TTL:             86400,
		})
		if err != nil {
			log.Printf("push send error to %s: %v", sub.Endpoint, err)
			continue
		}
		resp.Body.Close()

		// Clean up invalid subscriptions
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			log.Printf("removing expired push subscription: %s", sub.Endpoint)
			deletePushSubscription(s.db, sub.Endpoint)
		}
	}

	log.Printf("push: sent %d notifications for trip %s", len(subs), tripID)
}
