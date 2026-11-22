package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yyvfuruta/bt/internal/bluez"
	"github.com/yyvfuruta/bt/internal/core"
)

func testModel(t *testing.T) model {
	t.Helper()
	s, err := core.Load(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	return model{ctx: context.Background(), store: s, requests: core.NewRequests(time.Second), adapter: "/org/bluez/hci0", width: 100, height: 30, filter: "all", busy: map[string]string{}}
}
func TestSelectionSurvivesEvents(t *testing.T) {
	m := testModel(t)
	a := core.Device{Path: "a", Adapter: m.adapter, Address: "AA", Name: "Fone"}
	b := core.Device{Path: "b", Adapter: m.adapter, Address: "BB", Name: "Mouse"}
	updated, _ := m.Update(snapshotMsg{s: bluez.Snapshot{Devices: []core.Device{b, a}}})
	m = updated.(model)
	m.selected = "b"
	b.Name = "A new name"
	b.Connected = true
	c := core.Device{Path: "c", Adapter: m.adapter, Address: "CC", Name: "New"}
	updated, _ = m.Update(snapshotMsg{s: bluez.Snapshot{Devices: []core.Device{c, b, a}}})
	m = updated.(model)
	if m.selected != "b" || m.devices[1].Path != "b" {
		t.Fatalf("selection moved: %+v", m)
	}
	if !strings.Contains(m.event, "informativo") {
		t.Fatal("connection mistaken for request")
	}
	updated, _ = m.Update(snapshotMsg{s: bluez.Snapshot{Devices: []core.Device{a, c}}})
	m = updated.(model)
	if _, ok := m.device(); ok {
		t.Fatal("removed selection silently targeted another device")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.selected != "a" {
		t.Fatal("navigation failed", m.selected)
	}
}
func TestPendingRequestsDoNotBlockNavigation(t *testing.T) {
	m := testModel(t)
	m.devices = []core.Device{{Path: "a", Adapter: m.adapter}, {Path: "b", Adapter: m.adapter}}
	m.selected = "a"
	done := make(chan core.Answer, 1)
	go func() { done <- m.requests.Ask("a", "confirm", "Confirme 123456") }()
	deadline := time.Now().Add(time.Second)
	var r core.Request
	for time.Now().Before(deadline) {
		rs := m.requests.Snapshot()
		if len(rs) > 0 {
			r = rs[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if r.ID == 0 {
		t.Fatal("missing request")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.selected != "b" || m.requests.Snapshot()[0].State != "pending" {
		t.Fatal("request blocked navigation or auto accepted")
	}
	// y outside the request panel must never approve anything.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.requests.Snapshot()[0].State != "pending" {
		t.Fatal("accepted without request focus")
	}
	m.switchResource(requestsResource)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !strings.Contains(m.View(), "Confirme 123456") {
		t.Fatal("confirmation code not visible")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if a := <-done; a.State != "rejected" {
		t.Fatal(a)
	}
}
func TestForgetRequiresTypedConfirmation(t *testing.T) {
	m := testModel(t)
	m.devices = []core.Device{{Path: "a", Adapter: m.adapter}}
	m.selected = "a"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = updated.(model)
	if cmd != nil || m.mode != "forget" {
		t.Fatal("forget ran before confirmation")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || m.mode != "" || m.busy["a"] != "" {
		t.Fatal("empty confirmation removed device")
	}
}
