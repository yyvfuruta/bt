package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/yyvfuruta/bt/internal/core"
)

func (m model) View() string {
	if m.width < 48 || m.height < 18 {
		lines := wrap("Amplie para 48×18. Ctrl+C sai.", max(1, m.width))
		return strings.Join(lines[:min(len(lines), max(1, m.height))], "\n")
	}
	header := "bt · sem adaptador · ? ajuda"
	if a, ok := m.adapterValue(); ok {
		power := "desligado"
		if a.Powered {
			power = "ligado"
		}
		header = "bt · " + strings.TrimPrefix(a.Path, "/org/bluez/") + " · " + power
		if a.Discovering {
			header += " · buscando"
		}
		if len(m.adapters) > 1 {
			header += fmt.Sprintf(" · %d adaptadores", len(m.adapters))
		}
	}
	lines := []string{ansi.Truncate(core.Clean(header), m.width, "…")}
	rows := m.rows()
	position := 0
	for i, d := range rows {
		if d.Path == m.selected {
			position = i + 1
		}
	}
	focus := "> "
	if m.focusRequests {
		focus = "  "
	}
	filter := map[string]string{"all": "todos", "connected": "conectados", "paired": "pareados", "unpaired": "não pareados"}[m.filter]
	if m.showHelp {
		lines = append(lines, "Atalhos")
	} else {
		lines = append(lines, fmt.Sprintf("%sDispositivos · %s · %d/%d", focus, filter, position, len(rows)))
		if m.query != "" {
			lines = append(lines, ansi.Truncate("Busca: "+core.Clean(m.query), m.width, "…"))
		}
	}
	detail := ""
	if !m.showHelp {
		if d, ok := m.device(); ok {
			detail = "MAC " + d.Address
		} else if len(rows) > 0 {
			detail = "Use ↑↓ para selecionar novamente."
		}
	}
	requests := m.requestView()
	footer := m.footer()
	var messages []string
	if m.notice != "" {
		messages = limited(wrap("Aviso: "+m.notice, m.width), 3, m.width)
	} else if m.event != "" {
		event := strings.TrimSuffix(m.event, " (evento informativo, sem resposta)")
		messages = limited(wrap("Evento: "+event+" · informativo", m.width), 2, m.width)
	}
	// Reserve the request and input areas before choosing how many devices fit.
	fixed := len(lines) + 1 + len(requests) + len(messages) + 1 + len(footer)
	if detail != "" {
		fixed++
	}
	if m.height-fixed < 3 && len(messages) > 1 {
		fixed -= len(messages) - 1
		messages = limited(messages, 1, m.width)
	}
	budget := max(1, m.height-fixed)
	var body []string
	if m.showHelp {
		for _, s := range []string{
			"Enter conectar/desconectar",
			"c conectar · d desconectar",
			"s busca · / buscar nome · f filtrar",
			"p parear · x cancelar pareamento",
			"n apelido · F esquecer",
			"a adaptador · o ligar/desligar",
			"Tab trocar painel · ↑↓ navegar",
			"y aceitar · n rejeitar (nos pedidos)",
			"Esc limpar filtros · q ou Ctrl+C sair",
		} {
			body = append(body, wrap(s, m.width)...)
		}
		body = limited(body, budget, m.width)
	} else {
		body = m.deviceViewport(rows, budget)
	}
	lines = append(lines, body...)
	for i := len(body); i < budget; i++ {
		lines = append(lines, "")
	}
	if detail != "" {
		lines = append(lines, detail)
	}
	lines = append(lines, "")
	lines = append(lines, requests...)
	lines = append(lines, messages...)
	lines = append(lines, "")
	lines = append(lines, footer...)
	return strings.Join(lines, "\n")
}

func (m model) deviceLines(d core.Device, budget int) []string {
	marker := "  "
	if d.Path == m.selected {
		marker = "> "
	}
	name := limited(wrap(d.Label(), m.width-2), 2, m.width-2)
	connected := "desconectado"
	if d.Connected {
		connected = "conectado"
	}
	paired := "não pareado"
	if d.Paired {
		paired = "pareado"
	}
	battery := "bateria —"
	if d.Battery != nil {
		battery = fmt.Sprintf("bateria %d%%", *d.Battery)
	}
	metadata := readableType(d.Type) + " · " + connected + " · " + paired + " · " + battery
	if action := m.busy[d.Path]; action != "" {
		label := map[string]string{"connect": "conectando…", "disconnect": "desconectando…", "pair": "pareando…", "cancel": "cancelando…", "forget": "esquecendo…"}[action]
		if label != "" {
			metadata += " · " + label
		}
	}
	state := wrap(metadata, m.width-4)
	if len(name)+len(state) > budget {
		name = limited(name, 1, m.width-2)
	}
	lines := []string{marker + name[0]}
	for _, s := range name[1:] {
		lines = append(lines, "  "+s)
	}
	for _, s := range state {
		lines = append(lines, "    "+s)
	}
	return limited(lines, budget, m.width)
}

func (m model) deviceViewport(rows []core.Device, budget int) []string {
	if len(rows) == 0 {
		return limited(wrap("Nenhum dispositivo. s busca; o liga Bluetooth.", m.width), budget, m.width)
	}
	selected := 0
	for i, d := range rows {
		if d.Path == m.selected {
			selected = i
		}
	}
	items := make([][]string, len(rows))
	for i, d := range rows {
		items[i] = m.deviceLines(d, budget)
	}
	start, end, used := selected, selected+1, len(items[selected])
	for start > 0 && used+len(items[start-1]) <= budget/2 {
		start--
		used += len(items[start])
	}
	for end < len(items) && used+len(items[end]) <= budget {
		used += len(items[end])
		end++
	}
	for start > 0 && used+len(items[start-1]) <= budget {
		start--
		used += len(items[start])
	}
	var lines []string
	for _, item := range items[start:end] {
		lines = append(lines, item...)
	}
	return lines
}

func (m model) requestView() []string {
	rs := m.requests.Snapshot()
	pending, index := 0, -1
	for i, r := range rs {
		if r.State == "pending" {
			pending++
			if index < 0 {
				index = i
			}
		}
	}
	if m.focusRequests {
		for i, r := range rs {
			if r.ID == m.requestID {
				index = i
			}
		}
	}
	if index < 0 && len(rs) > 0 {
		index = len(rs) - 1
	}
	title := "  Pedidos"
	if m.focusRequests {
		title = "> Pedidos"
	}
	if pending == 0 {
		title += " · nenhuma pendente"
	} else if pending == 1 {
		title += " · 1 pendente"
	} else {
		title += fmt.Sprintf(" · %d pendentes", pending)
	}
	if !m.focusRequests && pending > 0 {
		title += " · Tab responder"
	}
	lines := wrap(title, m.width)
	if index < 0 || m.showHelp {
		return lines
	}
	r := rs[index]
	state := requestState(r.State)
	if r.State == "pending" {
		left := max(0, time.Until(r.Deadline).Round(time.Second))
		state += " · expira em " + left.String()
	}
	if pending == 0 && !m.focusRequests {
		return append(lines, "Último pedido: "+state)
	}
	if m.focusRequests {
		state += fmt.Sprintf(" · %d/%d", index+1, len(rs))
	}
	lines = append(lines, wrap(state, m.width)...)
	name, identity := "Dispositivo indisponível", r.Device
	for _, d := range m.devices {
		if d.Path == r.Device {
			name = d.Label()
			identity = "MAC " + d.Address
			break
		}
	}
	lines = append(lines, ansi.Truncate(core.Clean(name), m.width, "…"))
	lines = append(lines, wrap(identity, m.width)...)
	return append(lines, limited(wrap(r.Detail, m.width), 2, m.width)...)
}

func (m model) footer() []string {
	if m.showHelp {
		return wrap("? ou Esc voltar · q sair", m.width)
	}
	if m.mode != "" {
		prompt := map[string]string{
			"search":   "Buscar · Enter aplicar · Esc voltar",
			"nickname": "Apelido (vazio remove) · Enter salvar · Esc voltar",
			"forget":   "Digite esquecer · Enter confirmar · Esc voltar",
			"answer":   "PIN/chave · Enter enviar · Esc voltar",
		}[m.mode]
		value := m.input
		if m.mode == "answer" {
			value = strings.Repeat("•", len([]rune(value)))
		}
		lines := wrap(prompt, m.width)
		return append(lines, "> "+ansi.TruncateLeft(core.Clean(value), m.width-3, "…")+"▏")
	}
	if m.focusRequests {
		return append(wrap("y aceitar · n rejeitar · Tab voltar", m.width), wrap("↑↓ pedidos · ? ajuda · q sair", m.width)...)
	}
	action := "conectar"
	if d, ok := m.device(); ok && d.Connected {
		action = "desconectar"
	}
	return append(wrap("↑↓ navegar · Enter "+action+" · / buscar", m.width), wrap("s busca · Tab pedidos · ? ajuda · q sair", m.width)...)
}

func readableType(kind string) string {
	labels := map[string]string{"audio-headset": "fone de ouvido", "audio-headphones": "fone de ouvido", "audio-card": "áudio", "audio-speakers": "alto-falante", "input-keyboard": "teclado", "input-mouse": "mouse", "input-gaming": "controle", "phone": "telefone", "computer": "computador"}
	if label := labels[kind]; label != "" {
		return label
	}
	if kind == "" {
		return "desconhecido"
	}
	return core.Clean(kind)
}
func requestState(state string) string {
	return map[string]string{"pending": "Pendente", "accepted": "Aceito", "rejected": "Rejeitado", "expired": "Expirado", "canceled": "Cancelado", "info": "Informativo · sem resposta"}[state]
}
func wrap(s string, width int) []string {
	return strings.Split(ansi.Wrap(core.Clean(s), max(1, width), ""), "\n")
}
func limited(lines []string, n, width int) []string {
	if len(lines) <= n {
		return lines
	}
	if n <= 0 {
		return nil
	}
	out := append([]string(nil), lines[:n]...)
	out[n-1] = ansi.Truncate(out[n-1], max(0, width-1), "") + "…"
	return out
}
