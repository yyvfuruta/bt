package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yyvfuruta/bt/internal/bluez"
	"github.com/yyvfuruta/bt/internal/core"
	"golang.org/x/term"
)

type tick time.Time
type refresh struct{}
type ownerLost struct{}
type snapshotMsg struct {
	s   bluez.Snapshot
	err error
}
type resultMsg struct {
	action string
	device core.Device
	err    error
}

type Options struct{ Adapter, Query, Filter string }

type model struct {
	ctx                        context.Context
	client                     *bluez.Client
	store                      *core.Store
	requests                   *core.Requests
	adapters                   []bluez.Adapter
	devices                    []core.Device
	adapter                    string
	selected                   string
	requestID                  uint64
	focusRequests              bool
	showHelp                   bool
	query, filter, mode, input string
	editDevice                 core.Device
	editRequest                uint64
	notice                     string
	event                      string
	width, height              int
	refreshing                 bool
	agentReady                 bool
	busy                       map[string]string
}

func Run(ctx context.Context, c *bluez.Client, store *core.Store, options Options) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("a interface requer um terminal; use bt devices --json ou bt status --json")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := core.NewRequests(90 * time.Second)
	setupCtx, setupCancel := context.WithTimeout(ctx, 5*time.Second)
	ready, err := c.RegisterAgent(setupCtx, queue, true, true)
	setupCancel()
	notice := ""
	if err != nil {
		notice = "Agente: " + err.Error() + ". Saia e abra bt novamente para tentar registrar."
	}
	if options.Filter == "" {
		options.Filter = "all"
	}
	m := model{ctx: ctx, client: c, store: store, requests: queue, adapter: options.Adapter, query: options.Query, filter: options.Filter, notice: notice, width: 100, height: 30, agentReady: ready, busy: map[string]string{}, refreshing: true}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-c.Signals:
				if !ok {
					return
				}
				if sig == nil {
					continue
				}
				if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
					p.Send(ownerLost{})
				}
				p.Send(refresh{})
			}
		}
	}()
	_, err = p.Run()
	queue.Close()
	if ctx.Err() != nil {
		return nil
	}
	return err
}
func ticker() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tick(t) }) }
func (m model) load() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 4*time.Second)
		defer cancel()
		s, err := m.client.Snapshot(ctx)
		return snapshotMsg{s, err}
	}
}
func (m model) Init() tea.Cmd { return tea.Batch(m.load(), ticker()) }
func (m model) rows() []core.Device {
	ds := []core.Device{}
	for _, d := range m.devices {
		if d.Adapter == m.adapter {
			ds = append(ds, d)
		}
	}
	return core.Filter(ds, m.query, m.filter)
}
func (m model) device() (core.Device, bool) {
	for _, d := range m.rows() {
		if d.Path == m.selected {
			return d, true
		}
	}
	return core.Device{}, false
}
func (m model) adapterValue() (bluez.Adapter, bool) {
	for _, a := range m.adapters {
		if a.Path == m.adapter {
			return a, true
		}
	}
	return bluez.Adapter{}, false
}
func (m *model) selectFirst() {
	rows := m.rows()
	m.selected = ""
	if len(rows) > 0 {
		m.selected = rows[0].Path
	}
}
func (m model) act(d core.Device, action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 2*time.Minute)
		defer cancel()
		return resultMsg{action, d, m.client.Action(ctx, d, action)}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case ownerLost:
		m.client.ServiceLost()
		m.requests.Cancel()
		m.agentReady = false
		m.notice = "BlueZ saiu ou reiniciou; saia e abra bt para registrar o agente novamente."
	case refresh:
		if !m.refreshing {
			m.refreshing = true
			return m, m.load()
		}
	case tick:
		if m.client.Agent != nil && !m.client.Agent.Active.Load() && m.agentReady {
			m.agentReady = false
			m.notice = "Agente liberado pelo BlueZ; saia e abra bt novamente antes de parear/conectar."
		}
		if !m.client.Conn.Connected() {
			m.notice = "D-Bus desconectado; saia e abra bt novamente."
			m.requests.Cancel()
			m.agentReady = false
		}
		if !m.refreshing {
			m.refreshing = true
			return m, tea.Batch(m.load(), ticker())
		}
		return m, ticker()
	case snapshotMsg:
		m.refreshing = false
		if v.err != nil {
			m.notice = v.err.Error()
			m.devices = nil
			m.adapters = nil
			return m, nil
		}
		previous := map[string]bool{}
		for _, d := range m.devices {
			previous[d.Path] = d.Connected
		}
		for _, d := range v.s.Devices {
			if old, ok := previous[d.Path]; ok && old != d.Connected {
				state := "desconectou"
				if d.Connected {
					state = "conectou"
				}
				m.event = d.Label() + " " + state + " (evento informativo, sem resposta)"
			}
		}
		if err := m.store.Update(v.s.Devices, nil, nil, false); err != nil {
			m.notice = "Não foi possível salvar nomes: " + err.Error()
		}
		m.store.Decorate(v.s.Devices)
		first := m.selected == "" && len(m.devices) == 0
		m.devices = core.Stable(m.devices, v.s.Devices)
		m.adapters = v.s.Adapters
		if m.adapter == "" && len(m.adapters) > 0 {
			m.adapter = m.adapters[0].Path
		}
		if first {
			m.selectFirst()
		}
	case resultMsg:
		delete(m.busy, v.device.Path)
		if v.err != nil {
			m.notice = v.err.Error()
		} else {
			m.notice = v.action + ": concluído"
			if v.action == "forget" {
				if err := m.store.Update(nil, &v.device, nil, true); err != nil {
					m.notice = "Removido do BlueZ, mas cache: " + err.Error()
				}
			}
		}
		if !m.refreshing {
			m.refreshing = true
			return m, m.load()
		}
		return m, nil
	case tea.KeyMsg:
		key := v.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.showHelp {
			switch key {
			case "?", "esc":
				m.showHelp = false
			case "q":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.mode != "" {
			switch key {
			case "esc":
				m.mode = ""
				m.input = ""
			case "backspace", "ctrl+h":
				runes := []rune(m.input)
				if len(runes) > 0 {
					m.input = string(runes[:len(runes)-1])
				}
			case "enter":
				switch m.mode {
				case "search":
					m.query = m.input
					m.selectFirst()
				case "nickname":
					if err := m.store.Update(nil, &m.editDevice, &m.input, false); err != nil {
						m.notice = err.Error()
					} else {
						m.store.Decorate(m.devices)
						m.notice = "Apelido salvo"
					}
				case "forget":
					if m.input != "esquecer" {
						m.notice = "Remoção cancelada"
					} else {
						m.busy[m.editDevice.Path] = "forget"
						m.mode = ""
						return m, m.act(m.editDevice, "forget")
					}
				case "answer":
					if err := m.requests.Respond(m.editRequest, true, m.input); err != nil {
						m.notice = err.Error()
						return m, nil
					}
					m.notice = "Resposta enviada"
				}
				m.mode = ""
				m.input = ""
			default:
				if v.Type == tea.KeyRunes && len(m.input) < 256 {
					m.input += core.Clean(string(v.Runes))
				}
				if key == "space" {
					m.input += " "
				}
			}
			return m, nil
		}
		switch key {
		case "?":
			m.showHelp = true
		case "q":
			return m, tea.Quit
		case "tab":
			m.focusRequests = !m.focusRequests
			if m.focusRequests {
				rs := m.requests.Snapshot()
				if len(rs) > 0 {
					m.requestID = rs[len(rs)-1].ID
					for _, r := range rs {
						if r.State == "pending" {
							m.requestID = r.ID
							break
						}
					}
				}
			}
		case "up", "k", "down", "j":
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			if m.focusRequests {
				rs := m.requests.Snapshot()
				idx := -1
				for i, r := range rs {
					if r.ID == m.requestID {
						idx = i
					}
				}
				idx += delta
				if idx < 0 {
					idx = 0
				}
				if idx >= len(rs) {
					idx = len(rs) - 1
				}
				if idx >= 0 {
					m.requestID = rs[idx].ID
				}
			} else {
				rs := m.rows()
				idx := -1
				for i, d := range rs {
					if d.Path == m.selected {
						idx = i
					}
				}
				idx += delta
				if idx < 0 {
					idx = 0
				}
				if idx >= len(rs) {
					idx = len(rs) - 1
				}
				if idx >= 0 {
					m.selected = rs[idx].Path
				}
			}
		case "y", "n":
			if m.focusRequests {
				var r core.Request
				for _, item := range m.requests.Snapshot() {
					if item.ID == m.requestID {
						r = item
					}
				}
				if r.State != "pending" {
					m.notice = "Este item é informativo ou já foi encerrado"
					return m, nil
				}
				if key == "y" && (r.Kind == "pin" || r.Kind == "passkey") {
					m.mode = "answer"
					m.input = ""
					m.editRequest = r.ID
					return m, nil
				}
				if err := m.requests.Respond(r.ID, key == "y", ""); err != nil {
					m.notice = err.Error()
				} else {
					m.notice = "Resposta enviada"
				}
			} else if key == "n" {
				if d, ok := m.device(); ok {
					m.mode = "nickname"
					m.input = d.Nickname
					m.editDevice = d
				}
			}
		case "/":
			m.mode = "search"
			m.input = m.query
		case "esc":
			m.query = ""
			m.filter = "all"
			m.selectFirst()
		case "f":
			filters := []string{"all", "connected", "paired", "unpaired"}
			for i, f := range filters {
				if f == m.filter {
					m.filter = filters[(i+1)%len(filters)]
					break
				}
			}
			m.selectFirst()
		case "a":
			if len(m.adapters) > 0 {
				idx := 0
				for i, a := range m.adapters {
					if a.Path == m.adapter {
						idx = (i + 1) % len(m.adapters)
					}
				}
				m.adapter = m.adapters[idx].Path
				m.selectFirst()
			}
		case "s", "o":
			a, ok := m.adapterValue()
			if !ok {
				m.notice = "Adaptador indisponível; conecte um adaptador ou use a para alternar"
				return m, nil
			}
			if m.busy[a.Path] != "" {
				m.notice = "Operação no adaptador em andamento"
				return m, nil
			}
			m.busy[a.Path] = key
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
				defer cancel()
				var err error
				if key == "o" {
					err = m.client.Power(ctx, a, !a.Powered)
				} else if m.client.Scanning(a.Path) {
					err = m.client.StopScan(ctx, a)
				} else {
					err = m.client.StartScan(ctx, a)
				}
				return resultMsg{key, core.Device{Path: a.Path}, err}
			}
		case "enter", "c", "d", "p", "x", "F":
			if m.focusRequests {
				return m, nil
			}
			d, ok := m.device()
			if !ok {
				m.notice = "Selecione um dispositivo com as setas"
				return m, nil
			}
			action := map[string]string{"c": "connect", "d": "disconnect", "p": "pair", "x": "cancel", "F": "forget"}[key]
			if key == "enter" {
				action = "connect"
				if d.Connected {
					action = "disconnect"
				}
			}
			if (action == "pair" || action == "connect") && (!m.agentReady || m.client.Agent == nil || !m.client.Agent.Active.Load()) {
				m.notice = "Agente indisponível; saia e abra bt novamente antes de parear/conectar"
				return m, nil
			}
			if action != "cancel" && m.busy[d.Path] != "" {
				m.notice = "Operação em andamento neste dispositivo; x cancela pareamento"
				return m, nil
			}
			if action == "forget" {
				m.mode = "forget"
				m.input = ""
				m.editDevice = d
				return m, nil
			}
			m.busy[d.Path] = action
			return m, m.act(d, action)
		}
	}
	return m, nil
}
