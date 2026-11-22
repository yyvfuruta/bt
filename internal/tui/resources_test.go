package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yyvfuruta/bt/internal/bluez"
)

func TestResourceCommandsAndIndependentFilters(t *testing.T) {
	m := layoutModel(t)
	for command, want := range map[string]resourceKind{"devices": devicesResource, "dev": devicesResource, "conn": connectionsResource, "ad": adaptersResource, "req": requestsResource} {
		m.input = command
		if err := m.openResourceCommand(); err != nil || m.resourceKind() != want {
			t.Fatalf("%s: %v", command, err)
		}
	}
	m.input = "devices fone"
	if err := m.openResourceCommand(); err != nil {
		t.Fatal(err)
	}
	if len(m.rows()) != 1 || m.query != "fone" {
		t.Fatal("device search")
	}
	m.switchResource(adaptersResource)
	if m.query != "" {
		t.Fatal("device query leaked")
	}
	m.query = "hci0"
	if len(m.adapterRows()) != 1 {
		t.Fatal("adapter path search")
	}
	m.switchResource(devicesResource)
	if m.query != "fone" {
		t.Fatal("query not restored")
	}
	m.input = "unknown"
	if err := m.openResourceCommand(); err == nil {
		t.Fatal("unknown resource accepted")
	}
}
func TestCommandChooserAndTabNavigation(t *testing.T) {
	m := layoutModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = updated.(model)
	if m.mode != "command" || !strings.Contains(m.View(), "connections") {
		t.Fatal("chooser not shown")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("conn")})
	m = updated.(model)
	if !strings.Contains(m.View(), "> conn▏") {
		t.Fatal("typed command is invisible")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.resourceKind() != connectionsResource || m.mode != "" {
		t.Fatal("command did not navigate")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.resourceKind() != adaptersResource {
		t.Fatal("Tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	if m.resourceKind() != connectionsResource {
		t.Fatal("Shift+Tab")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.resourceKind() != devicesResource {
		t.Fatal("Esc")
	}
}
func TestResourceSearchAndConnectionLifecycle(t *testing.T) {
	m := layoutModel(t)
	m.switchResource(connectionsResource)
	if len(m.rows()) != 1 || m.rows()[0].Path != "headphones" {
		t.Fatal("connections fabricated")
	}
	m.devices[0].Connected = false
	if len(m.rows()) != 0 {
		t.Fatal("disconnected device is a connection")
	}
	m.switchResource(adaptersResource)
	m.adapters = append(m.adapters, bluez.Adapter{Path: "/org/bluez/hci1", Address: "BB", Name: "USB", Powered: false})
	m.query = "usb"
	if rows := m.adapterRows(); len(rows) != 1 || rows[0].Address != "BB" {
		t.Fatal("adapter name search")
	}
	pendingConfirmation(t, &m)
	m.switchResource(requestsResource)
	m.query = "000042"
	if len(m.requestRows()) != 1 {
		t.Fatal("code search")
	}
	m.query = "teclado"
	if len(m.requestRows()) != 1 {
		t.Fatal("device name search")
	}
	m.query = "not found"
	if len(m.requestRows()) != 0 {
		t.Fatal("request filter")
	}
}

func TestLongDetailsScrollWithoutChangingSelection(t *testing.T) {
	m := layoutModel(t)
	m.width, m.height = 48, 18
	m.devices[0].Name = strings.Repeat("Nome comprido 🎧 ", 25)
	m.showDetails = true
	for i := 0; i < 100; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	view := assertFits(t, m)
	if m.selected != "headphones" || !strings.Contains(view, "/org/bluez/hci0") {
		t.Fatalf("detail scrolling failed:\n%s", view)
	}
	for i := 0; i < 100; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(model)
	}
	if m.detailScroll != 0 {
		t.Fatal("could not scroll back")
	}
}
