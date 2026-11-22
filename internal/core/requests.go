package core

import (
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

type Request struct {
	ID       uint64    `json:"id"`
	Device   string    `json:"device"`
	Kind     string    `json:"kind"`
	Detail   string    `json:"detail"`
	Deadline time.Time `json:"deadline"`
	State    string    `json:"state"`
}
type Answer struct {
	Value string
	State string
}
type pending struct {
	request Request
	answer  chan Answer
}
type Requests struct {
	mu     sync.Mutex
	next   uint64
	items  []*pending
	TTL    time.Duration
	closed bool
}

func NewRequests(ttl time.Duration) *Requests { return &Requests{TTL: ttl} }

func (q *Requests) trim() {
	if len(q.items) < 64 {
		return
	}
	for i, p := range q.items {
		if p.request.State != "pending" {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return
		}
	}
}
func (q *Requests) Ask(device, kind, detail string) Answer {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return Answer{State: "canceled"}
	}
	count := 0
	for _, p := range q.items {
		if p.request.State == "pending" {
			count++
		}
	}
	if count >= 16 {
		q.mu.Unlock()
		return Answer{State: "rejected"}
	}
	q.next++
	q.trim()
	p := &pending{Request{q.next, device, kind, detail, time.Now().Add(q.TTL), "pending"}, make(chan Answer, 1)}
	q.items = append(q.items, p)
	q.mu.Unlock()
	timer := time.NewTimer(q.TTL)
	defer timer.Stop()
	select {
	case a := <-p.answer:
		return a
	case <-timer.C:
		q.mu.Lock()
		if p.request.State == "pending" {
			p.request.State = "expired"
			p.answer <- Answer{State: "expired"}
		}
		q.mu.Unlock()
		return <-p.answer
	}
}
func (q *Requests) Info(device, detail string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	// DisplayPasskey can update for each digit typed remotely.
	for _, p := range q.items {
		if p.request.State == "info" && p.request.Device == device {
			p.request.Detail = detail
			return
		}
	}
	q.next++
	q.trim()
	q.items = append(q.items, &pending{request: Request{ID: q.next, Device: device, Kind: "display", Detail: detail, State: "info", Deadline: time.Now().Add(q.TTL)}})
}
func (q *Requests) Snapshot() []Request {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []Request{}
	for _, p := range q.items {
		if p.request.State == "info" && time.Now().After(p.request.Deadline) {
			p.request.State = "expired"
		}
		out = append(out, p.request)
	}
	return out
}
func (q *Requests) Respond(id uint64, accept bool, value string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, p := range q.items {
		if p.request.ID != id {
			continue
		}
		if p.request.State != "pending" {
			return fmt.Errorf("solicitação já %s", p.request.State)
		}
		if !time.Now().Before(p.request.Deadline) {
			p.request.State = "expired"
			p.answer <- Answer{State: "expired"}
			return fmt.Errorf("solicitação expirada")
		}
		if accept {
			switch p.request.Kind {
			case "pin":
				if !utf8.ValidString(value) || len(value) < 1 || len(value) > 16 {
					return fmt.Errorf("PIN deve ter de 1 a 16 bytes")
				}
			case "passkey":
				if len(value) < 1 || len(value) > 6 {
					return fmt.Errorf("chave deve conter de 1 a 6 dígitos")
				}
				for _, r := range value {
					if r < '0' || r > '9' {
						return fmt.Errorf("chave deve conter somente dígitos")
					}
				}
				if _, err := strconv.ParseUint(value, 10, 32); err != nil {
					return err
				}
			}
		}
		state := "rejected"
		if accept {
			state = "accepted"
		}
		p.request.State = state
		p.answer <- Answer{value, state}
		return nil
	}
	return fmt.Errorf("solicitação não encontrada")
}
func (q *Requests) Cancel() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancel()
}

// Close also rejects calls racing with shutdown or Agent1.Release.
func (q *Requests) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cancel()
}
func (q *Requests) cancel() {
	for _, p := range q.items {
		if p.request.State == "pending" {
			p.request.State = "canceled"
			p.answer <- Answer{State: "canceled"}
		}
		if p.request.State == "info" {
			p.request.State = "canceled"
		}
	}
}
