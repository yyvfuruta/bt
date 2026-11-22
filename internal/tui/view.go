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
	header := "bt · sem adaptador"
	if a, ok := m.adapterValue(); ok {
		header = "bt · " + strings.TrimPrefix(a.Path, "/org/bluez/")
		if !a.Powered {
			header += " · desligado (o liga)"
		} else if a.Discovering {
			header += " · buscando…"
		}
	}
	label := string(m.resourceKind())
	count := m.resourceCount()
	if m.showDetails {
		label += " / detalhes"
	}
	if m.showHelp {
		label = "ajuda"
	}
	if m.mode == "command" {
		label = "recursos"
		count = len(resourceMatches(m.input))
	}
	if !m.showDetails && !m.showHelp || m.mode == "command" {
		label += fmt.Sprintf(" [%d]", count)
	}
	lines := []string{ansi.Truncate(header, m.width, "…"), ansi.Truncate(label, m.width, "…")}
	footer := m.footer()
	messages := m.messages()
	banner := m.requestBanner()
	budget := m.contentHeight()
	content := m
	content.width -= 4
	var body []string
	switch {
	case m.mode == "command":
		body = content.resourceChooser()
	case m.showHelp:
		all := content.helpLines()
		start := min(m.detailScroll, max(0, len(all)-budget))
		body = all[start:min(len(all), start+budget)]
	case m.showDetails:
		all := content.detailLines()
		start := min(m.detailScroll, max(0, len(all)-budget))
		body = all[start:min(len(all), start+budget)]
	case m.resourceKind() == adaptersResource:
		selected := 0
		items := [][]string{}
		for i, a := range m.adapterRows() {
			if a.Path == m.adapterCursor {
				selected = i
			}
			name := core.Clean(a.Name)
			if name == "" {
				name = strings.TrimPrefix(a.Path, "/org/bluez/")
			}
			state := "desligado"
			if a.Powered {
				state = "ligado"
			}
			if a.Path == m.adapter {
				state += " · ativo"
			}
			items = append(items, content.listItem(a.Path == m.adapterCursor, name+" · "+state))
		}
		body = viewport(items, selected, budget)
	case m.resourceKind() == requestsResource:
		selected := 0
		items := [][]string{}
		for i, r := range m.requestRows() {
			if r.ID == m.requestID {
				selected = i
			}
			name, _ := m.requestIdentity(r)
			items = append(items, content.listItem(r.ID == m.requestID, name+" · "+requestState(r.State)))
		}
		body = viewport(items, selected, budget)
		if m.event != "" {
			event := strings.TrimSuffix(m.event, " (evento informativo, sem resposta)")
			body = append(body, wrap("Evento informativo: "+event, content.width)...)
		}
	default:
		body = content.deviceViewport(m.rows(), budget)
	}
	if len(body) == 0 {
		body = wrap("Nenhum registro. / filtra; : troca recurso.", content.width)
	}
	body = limited(body, budget, content.width)
	lines = append(lines, "╭"+strings.Repeat("─", m.width-2)+"╮")
	for i := 0; i < budget; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, "│ "+line+strings.Repeat(" ", max(0, content.width-ansi.StringWidth(line)))+" │")
	}
	lines = append(lines, "╰"+strings.Repeat("─", m.width-2)+"╯")
	lines = append(lines, banner...)
	lines = append(lines, messages...)
	lines = append(lines, footer...)
	return strings.Join(lines, "\n")
}

func (m model) contentHeight() int {
	return max(1, m.height-4-len(m.footer())-len(m.messages())-len(m.requestBanner()))
}
func (m model) resourceCount() int {
	switch m.resourceKind() {
	case adaptersResource:
		return len(m.adapterRows())
	case requestsResource:
		return len(m.requestRows())
	default:
		return len(m.rows())
	}
}
func (m model) messages() []string {
	if m.notice != "" {
		return limited(wrap(m.notice, m.width), 2, m.width)
	}
	if m.mode != "command" {
		filters := []string{}
		if m.query != "" {
			filters = append(filters, m.query)
		}
		if (m.resourceKind() == devicesResource || m.resourceKind() == connectionsResource) && m.filter != "" && m.filter != "all" {
			filters = append(filters, m.filter)
		}
		if len(filters) > 0 {
			return []string{ansi.Truncate("Filtro: "+core.Clean(strings.Join(filters, " · ")), m.width, "…")}
		}
	}
	return nil
}
func (m model) requestBanner() []string {
	if m.focusRequests {
		return nil
	}
	pending, info := 0, false
	for _, r := range m.requests.Snapshot() {
		if r.State == "pending" {
			pending++
		}
		if r.State == "info" {
			info = true
		}
	}
	if pending > 0 {
		if pending == 1 {
			return wrap("1 pedido pendente · :requests", m.width)
		}
		return wrap(fmt.Sprintf("%d pedidos pendentes · :requests", pending), m.width)
	}
	if info {
		return wrap("Instruções de pareamento · :requests", m.width)
	}
	return nil
}
func (m model) listItem(selected bool, text string) []string {
	lines := limited(wrap(text, m.width-2), 2, m.width-2)
	for i := range lines {
		prefix := "  "
		if i == 0 && selected {
			prefix = "> "
		}
		lines[i] = prefix + lines[i]
	}
	return lines
}
func (m model) deviceViewport(rows []core.Device, budget int) []string {
	if len(rows) == 0 {
		text := "Nenhum dispositivo. s inicia busca."
		if m.resourceKind() == connectionsResource {
			text = "Nenhuma conexão ativa."
		}
		return wrap(text, m.width)
	}
	items := [][]string{}
	selected := 0
	for i, d := range rows {
		if d.Path == m.selected {
			selected = i
		}
		label := d.Label()
		if d.Connected {
			label += " · conectado"
		}
		if d.Battery != nil {
			label += fmt.Sprintf(" · %d%%", *d.Battery)
		}
		if action := m.busy[d.Path]; action != "" {
			label += " · " + action
		}
		items = append(items, m.listItem(d.Path == m.selected, label))
	}
	return viewport(items, selected, budget)
}
func viewport(items [][]string, selected, budget int) []string {
	if len(items) == 0 {
		return nil
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

func (m model) detailLines() []string {
	var fields []string
	switch m.resourceKind() {
	case adaptersResource:
		a, ok := m.selectedAdapter()
		if !ok {
			return []string{"Adaptador removido; Esc volta à lista."}
		}
		fields = []string{a.Name, "MAC: " + a.Address, "Caminho: " + a.Path, fmt.Sprintf("Ligado: %t", a.Powered), fmt.Sprintf("Buscando: %t", a.Discovering), "o liga/desliga · s busca · a usa este adaptador"}
	case requestsResource:
		r, ok := m.request()
		if !ok {
			return []string{"Pedido indisponível; Esc volta à lista."}
		}
		name, identity := m.requestIdentity(r)
		state := requestState(r.State)
		if r.State == "pending" {
			state += " · expira em " + max(0, time.Until(r.Deadline).Round(time.Second)).String()
		}
		fields = []string{name, identity, state, r.Detail}
		if r.State == "pending" {
			fields = append(fields, "y aceita · n rejeita")
		}
	default:
		d, ok := m.device()
		if !ok {
			return []string{"Dispositivo removido; Esc volta à lista."}
		}
		connected := "desconectado"
		if d.Connected {
			connected = "conectado"
		}
		paired := "não (p cria o vínculo)"
		if d.Paired {
			paired = "sim (vínculo salvo)"
		}
		battery := "não informada"
		if d.Battery != nil {
			battery = fmt.Sprintf("%d%%", *d.Battery)
		}
		fields = []string{d.Label(), "MAC: " + d.Address, "Conexão: " + connected, "Pareado: " + paired, "Tipo: " + readableType(d.Type), "Bateria: " + battery, "Adaptador: " + d.Adapter}
		if d.Name != "" && d.Name != d.Label() {
			fields = append(fields, "Nome original: "+d.Name)
		}
	}
	var lines []string
	for _, s := range fields {
		lines = append(lines, wrap(s, m.width)...)
	}
	return lines
}
func (m model) requestIdentity(r core.Request) (string, string) {
	for _, d := range m.devices {
		if d.Path == r.Device {
			return d.Label(), "MAC: " + d.Address
		}
	}
	return "Dispositivo indisponível", r.Device
}
func (m model) resourceChooser() []string {
	var lines []string
	for i, r := range resourceMatches(m.input) {
		lines = append(lines, m.listItem(i == m.commandIndex, string(r.kind)+" · "+r.label)...)
		lines = append(lines, wrap("  "+r.description, m.width)...)
	}
	if len(lines) == 0 {
		return []string{"Recurso desconhecido."}
	}
	return lines
}
func (m model) helpLines() []string {
	var lines []string
	for _, s := range []string{": escolher tipo de recurso", "/ filtrar registros da visão atual", "Tab próxima visão · Shift+Tab anterior", "Enter detalhes · Esc voltar", "c conectar · d desconectar", "p parear · x cancelar pareamento", "n apelido · F esquecer", "a adaptador · o ligar/desligar · s busca", "y aceitar · n rejeitar (no pedido aberto)", "q ou Ctrl+C sair"} {
		lines = append(lines, wrap(s, m.width)...)
	}
	return lines
}
func (m model) footer() []string {
	if m.showHelp {
		return wrap("↑↓ rolar · Esc voltar · q sair", m.width)
	}
	if m.mode != "" {
		prompt := map[string]string{"command": ": recurso · Enter abrir · Esc voltar", "search": "/ filtro · Enter aplicar · Esc voltar", "nickname": "Apelido (vazio remove) · Enter salvar", "forget": "Digite esquecer · Enter confirmar", "answer": "PIN/chave · Enter enviar · Esc voltar"}[m.mode]
		value := m.input
		if m.mode == "answer" {
			value = strings.Repeat("•", len([]rune(value)))
		}
		value = core.Clean(value)
		if skip := ansi.StringWidth(value) - (m.width - 3); skip > 0 {
			value = ansi.TruncateLeft(value, skip+1, "…")
		}
		return append(wrap(prompt, m.width), "> "+value+"▏")
	}
	if m.showDetails {
		if m.focusRequests {
			return append(wrap("y aceitar · n rejeitar", m.width), wrap("↑↓ rolar · Esc voltar · q sair", m.width)...)
		}
		if m.resourceKind() == adaptersResource {
			return wrap("a usar · o liga/desliga · Esc voltar", m.width)
		}
		action := "c conectar"
		if d, ok := m.device(); ok {
			if d.Connected {
				action = "d desconectar"
			}
			if !d.Paired {
				action += " · p parear"
			}
		}
		return append(wrap(action, m.width), wrap("↑↓ rolar · Esc voltar · ? ações", m.width)...)
	}
	return append(wrap("Enter detalhes · : recursos · / filtrar", m.width), wrap("Tab visões · ? ajuda · q sair", m.width)...)
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
