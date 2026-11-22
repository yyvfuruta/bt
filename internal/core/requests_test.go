package core

import (
	"sync"
	"testing"
	"time"
)

func waitPending(t *testing.T, q *Requests) Request {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, r := range q.Snapshot() {
			if r.State == "pending" {
				return r
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request was not queued")
	return Request{}
}
func TestRequestRequiresExplicitResponse(t *testing.T) {
	q := NewRequests(time.Second)
	done := make(chan Answer, 1)
	go func() { done <- q.Ask("device", "confirm", "123456") }()
	r := waitPending(t, q)
	select {
	case <-done:
		t.Fatal("automatically answered")
	default:
	}
	q.Info("device", "connected")
	if q.Snapshot()[0].State != "pending" {
		t.Fatal("event answered request")
	}
	if err := q.Respond(r.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if a := <-done; a.State != "rejected" {
		t.Fatal(a)
	}
	if err := q.Respond(r.ID, true, ""); err == nil {
		t.Fatal("double response accepted")
	}
}
func TestRequestExpiryAndCancel(t *testing.T) {
	q := NewRequests(15 * time.Millisecond)
	a := q.Ask("d", "confirm", "")
	if a.State != "expired" || q.Snapshot()[0].State != "expired" {
		t.Fatal(a)
	}
	if err := q.Respond(1, true, ""); err == nil {
		t.Fatal("expired request accepted")
	}
	q = NewRequests(time.Second)
	done := make(chan Answer, 1)
	go func() { done <- q.Ask("d", "authorize", "") }()
	waitPending(t, q)
	q.Cancel()
	if a = <-done; a.State != "canceled" {
		t.Fatal(a)
	}
}
func TestInputValidationAndConcurrentResolution(t *testing.T) {
	for _, kind := range []string{"pin", "passkey"} {
		q := NewRequests(time.Second)
		done := make(chan Answer, 1)
		go func() { done <- q.Ask("d", kind, "") }()
		r := waitPending(t, q)
		for _, bad := range []string{"", "12345678901234567"} {
			if err := q.Respond(r.ID, true, bad); err == nil {
				t.Fatalf("accepted invalid %s", kind)
			}
		}
		if kind == "passkey" {
			for _, bad := range []string{"-1", "1a", "1000000"} {
				if err := q.Respond(r.ID, true, bad); err == nil {
					t.Fatal("accepted", bad)
				}
			}
		}
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); _ = q.Respond(r.ID, true, "000001") }()
		go func() { defer wg.Done(); q.Cancel() }()
		go func() { defer wg.Done(); _ = q.Respond(r.ID, false, "") }()
		wg.Wait()
		a := <-done
		if a.State != "accepted" && a.State != "canceled" && a.State != "rejected" {
			t.Fatal(a)
		}
	}
}
