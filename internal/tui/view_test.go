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

func TestSimpleLayoutFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{48, 18}, {60, 20}, {80, 24}, {100, 30}, {140, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := layoutModel(t)
			m.width, m.height = size[0], size[1]
			for _, selected := range []int{0, 1, 2} {
				m.selected = m.devices[selected].Path
				view := assertFits(t, m)
				for _, text := range []string{m.devices[selected].Label(), "bateria", "q sair", m.devices[selected].Address} {
					if !strings.Contains(view, text) {
						t.Fatalf("selected device or footer missing %q:\n%s", text, view)
					}
				}
			}
			m.selected = "headphones"
			view := assertFits(t, m)
			for _, text := range []string{"fone de ouvido", "conectado", "pareado", "82%"} {
				if !strings.Contains(view, text) {
					t.Fatalf("metadata hidden %q:\n%s", text, view)
				}
			}
		})
	}
}

func TestPendingLayoutKeepsCodeIdentityAndActionsVisible(t *testing.T) {
	m := layoutModel(t)
	r := pendingConfirmation(t, &m)
	for _, size := range [][2]int{{48, 18}, {60, 20}, {80, 24}, {100, 30}} {
		for _, focus := range []bool{false, true} {
			for _, mode := range []string{"", "search", "nickname", "forget", "answer"} {
				m.width, m.height = size[0], size[1]
				m.focusRequests = focus
				m.requestID = r.ID
				m.query = "fone"
				m.notice = strings.Repeat("Falha de conexão. Tente novamente. ", 5)
				m.mode = mode
				m.input = strings.Repeat("🎧", 30)
				view := assertFits(t, m)
				for _, text := range []string{"000042", "02:00:00:00:00:02", "Pendente"} {
					if !strings.Contains(view, text) {
						t.Fatalf("request lost %q:\n%s", text, view)
					}
				}
				if focus && mode == "" && !strings.Contains(view, "n rejeitar") {
					t.Fatal("rejection hidden")
				}
				if mode != "" && !strings.HasSuffix(view, "▏") {
					t.Fatal("input cursor hidden")
				}
			}
		}
	}
}

func TestWrappingAndViewport(t *testing.T) {
	m := layoutModel(t)
	m.width = 48
	m.height = 18
	m.devices[1].Name = "Teclado 日本語 🎹 com um nome comprido que deve quebrar em linhas"
	m.devices[1].Type = "controle/periférico"
	m.selected = "keyboard"
	view := assertFits(t, m)
	for _, text := range []string{"日本語", "controle/periférico", "desconectado", "não pareado", "bateria —"} {
		if !strings.Contains(view, text) {
			t.Fatalf("text lost %q:\n%s", text, view)
		}
	}
	for i := 0; i < 20; i++ {
		m.devices = append(m.devices, core.Device{Path: fmt.Sprint(i), Adapter: m.adapter, Name: fmt.Sprintf("Dispositivo %d", i)})
	}
	m.selected = "19"
	if view = assertFits(t, m); !strings.Contains(view, "> Dispositivo 19") {
		t.Fatalf("selected row out of viewport:\n%s", view)
	}
}

func TestHelpAndEnter(t *testing.T) {
	m := layoutModel(t)
	m.width = 48
	m.height = 18
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(model)
	view := assertFits(t, m)
	for _, text := range []string{"c conectar", "d desconectar", "n apelido", "F esquecer", "o ligar/desligar"} {
		if !strings.Contains(view, text) {
			t.Fatalf("help missing %q:\n%s", text, view)
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("help allowed accidental device action")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.showHelp {
		t.Fatal("Esc did not close help")
	}
	m.client = &bluez.Client{Agent: &bluez.Agent{}}
	m.client.Agent.Active.Store(true)
	m.agentReady = true
	for _, connected := range []bool{false, true} {
		m.devices[0].Connected = connected
		m.busy = map[string]string{}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		next := updated.(model)
		want := "connect"
		if connected {
			want = "disconnect"
		}
		if cmd == nil || next.busy["headphones"] != want {
			t.Fatalf("Enter did not %s", want)
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
