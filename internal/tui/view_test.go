package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/yyvfuruta/bt/internal/bluez"
	"github.com/yyvfuruta/bt/internal/core"
)

func layoutModel(t *testing.T) model {
	t.Helper()
	m := testModel(t)
	m.adapters = []bluez.Adapter{{Path: m.adapter, Powered: true, Discovering: true}}
	battery := uint8(82)
	m.devices = []core.Device{
		{Path: "headphones", Adapter: m.adapter, Address: "02:00:00:00:00:01", Name: "Meu Fone", Type: "audio-headset", Connected: true, Paired: true, Battery: &battery},
		{Path: "keyboard", Adapter: m.adapter, Address: "02:00:00:00:00:02", Name: "Teclado", Type: "input-keyboard"},
		{Path: "unknown", Adapter: m.adapter, Address: "02:00:00:00:00:03", Type: "desconhecido"},
	}
	m.selected = m.devices[0].Path
	return m
}

func pendingConfirmation(t *testing.T, m *model) core.Request {
	t.Helper()
	m.requests = core.NewRequests(45 * time.Second)
	done := make(chan core.Answer, 1)
	go func() { done <- m.requests.Ask("keyboard", "confirm", "Confirme a chave 000042 nos dois dispositivos") }()
	t.Cleanup(func() { m.requests.Close(); <-done })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rs := m.requests.Snapshot()
		if len(rs) > 0 {
			return rs[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no pending confirmation")
	return core.Request{}
}

func assertFits(t *testing.T, m model) string {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("%dx%d: %d lines overflow terminal:\n%s", m.width, m.height, len(lines), view)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > m.width {
			t.Fatalf("%dx%d line %d has width %d: %q", m.width, m.height, i, width, line)
		}
	}
	return view
}

func TestResourceLayoutFitsAndKeepsMainViewQuiet(t *testing.T) {
	for _, size := range [][2]int{{48, 18}, {60, 20}, {80, 24}, {100, 30}, {140, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := layoutModel(t)
			m.width, m.height = size[0], size[1]
			view := assertFits(t, m)
			for _, text := range []string{"Meu Fone", "conectado", "82%", "devices [3]", ": recursos"} {
				if !strings.Contains(view, text) {
					t.Fatalf("missing %q:\n%s", text, view)
				}
			}
			for _, text := range []string{"Pareado:", "MAC:", "fone de ouvido", "bateria —", "nenhuma pendente"} {
				if strings.Contains(view, text) {
					t.Fatalf("main view cluttered by %q", text)
				}
			}
			m.showDetails = true
			view = assertFits(t, m)
			for _, text := range []string{"02:00:00:00:00:01", "Pareado: sim", "fone de ouvido", "82%"} {
				if !strings.Contains(view, text) {
					t.Fatalf("detail missing %q:\n%s", text, view)
				}
			}
			m.switchResource(connectionsResource)
			view = assertFits(t, m)
			if !strings.Contains(view, "connections [1]") || strings.Contains(view, "Teclado") {
				t.Fatal(view)
			}
			m.switchResource(adaptersResource)
			assertFits(t, m)
			m.showDetails = true
			assertFits(t, m)
		})
	}
}

func TestRequestsAreSeparateAndRemainVisibleAsNotification(t *testing.T) {
	m := layoutModel(t)
	r := pendingConfirmation(t, &m)
	for _, size := range [][2]int{{48, 18}, {60, 20}, {80, 24}, {100, 30}} {
		m.width, m.height = size[0], size[1]
		m.switchResource(devicesResource)
		view := assertFits(t, m)
		if !strings.Contains(view, ":requests") || strings.Contains(view, "000042") {
			t.Fatalf("request details leaked into main view:\n%s", view)
		}
		m.switchResource(requestsResource)
		view = assertFits(t, m)
		if !strings.Contains(view, "Pendente") {
			t.Fatal(view)
		}
		// An approval key in a list cannot approve a code that has not been opened.
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		m = updated.(model)
		if m.requests.Snapshot()[0].State != "pending" {
			t.Fatal("list approved unopened request")
		}
		m.notice = ""
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		for _, mode := range []string{"", "search", "nickname", "forget", "answer"} {
			m.mode = mode
			m.input = strings.Repeat("🎧", 30)
			m.requestID = r.ID
			view = assertFits(t, m)
			for _, text := range []string{"000042", "02:00:00:00:00:02", "Pendente"} {
				if !strings.Contains(view, text) {
					t.Fatalf("request missing %q:\n%s", text, view)
				}
			}
			if mode != "" && !strings.HasSuffix(view, "▏") {
				t.Fatal("input cursor hidden")
			}
		}
		m.mode = ""
	}
}

func TestViewportAndLongNames(t *testing.T) {
	m := layoutModel(t)
	m.width = 48
	m.height = 18
	m.devices[1].Name = "Teclado 日本語 🎹 com um nome comprido que deve quebrar em linhas"
	m.selected = "keyboard"
	view := assertFits(t, m)
	if !strings.Contains(view, "日本語") {
		t.Fatal(view)
	}
	for i := 0; i < 20; i++ {
		m.devices = append(m.devices, core.Device{Path: fmt.Sprint(i), Adapter: m.adapter, Name: fmt.Sprintf("Dispositivo %d", i)})
	}
	m.selected = "19"
	view = assertFits(t, m)
	if !strings.Contains(view, "> Dispositivo 19") {
		t.Fatal(view)
	}
	m.showDetails = true
	assertFits(t, m)
}

func TestHelpEnterAndExplicitDeviceActions(t *testing.T) {
	m := layoutModel(t)
	m.width = 48
	m.height = 18
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || !m.showDetails {
		t.Fatal("Enter must inspect without changing Bluetooth state")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(model)
	view := assertFits(t, m)
	if !strings.Contains(view, ": escolher") {
		t.Fatal(view)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("help allowed accidental action")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	m.client = &bluez.Client{Agent: &bluez.Agent{}}
	m.client.Agent.Active.Store(true)
	m.agentReady = true
	for key, want := range map[rune]string{'c': "connect", 'd': "disconnect"} {
		m.busy = map[string]string{}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd == nil || updated.(model).busy["headphones"] != want {
			t.Fatalf("action %s missing", want)
		}
	}
}

func TestNarrowTerminalMessageFits(t *testing.T) {
	m := testModel(t)
	for _, size := range [][2]int{{20, 10}, {1, 1}, {48, 17}} {
		m.width, m.height = size[0], size[1]
		assertFits(t, m)
	}
}
