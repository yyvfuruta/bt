package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/yyvfuruta/bt/internal/core"
)

type tableColumn struct {
	key, title string
	width      int // Zero is the flexible column; its width uses remaining space.
	priority   int // Zero is always visible; optional columns are added in priority order.
	wrap       bool
}
type tableRecord struct {
	selected bool
	values   map[string]string
	wrapName bool
}

// Column sizes depend only on terminal width, never on changing device data.
// Headers always fit in full, and names retain at least 17 terminal cells.
func fitColumns(width int, all []tableColumn) []tableColumn {
	visible := make([]bool, len(all))
	maxPriority := 0
	for i, c := range all {
		visible[i] = c.priority == 0
		maxPriority = max(maxPriority, c.priority)
	}
	required := func() int {
		count, total := 0, 2 // Selection marker.
		for i, c := range all {
			if visible[i] {
				count++
				if c.width == 0 {
					total += 17
				} else {
					total += max(c.width, ansi.StringWidth(c.title))
				}
			}
		}
		return total + max(0, count-1)*3
	}
	for priority := 1; priority <= maxPriority; priority++ {
		for i, c := range all {
			if c.priority == priority {
				visible[i] = true
				if required() > width {
					visible[i] = false
				}
			}
		}
	}
	columns := []tableColumn{}
	fixed := 2
	for i, c := range all {
		if visible[i] {
			if c.width > 0 {
				c.width = max(c.width, ansi.StringWidth(c.title))
				fixed += c.width
			}
			columns = append(columns, c)
		}
	}
	fixed += max(0, len(columns)-1) * 3
	for i := range columns {
		if columns[i].width == 0 {
			columns[i].width = max(1, width-fixed)
		}
	}
	return columns
}

func tableCell(value string, width int) string {
	value = ansi.Truncate(core.Clean(value), width, "…")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func renderTable(columns []tableColumn, records []tableRecord, budget, gap int, empty string) []string {
	headings, separators := []string{}, []string{}
	for _, c := range columns {
		headings = append(headings, tableCell(c.title, c.width))
		separators = append(separators, strings.Repeat("─", c.width))
	}
	lines := []string{"  " + strings.Join(headings, " │ "), "  " + strings.Join(separators, "─┼─")}
	if len(records) == 0 {
		return append(lines, wrap(empty, max(1, ansi.StringWidth(lines[0])))...)
	}
	items := [][]string{}
	selected := 0
	for index, r := range records {
		if r.selected {
			selected = index
		}
		cells := make([][]string, len(columns))
		height := 1
		for i, c := range columns {
			value := core.Clean(r.values[c.key])
			cells[i] = []string{value}
			if c.wrap || r.wrapName && c.key == "name" {
				cells[i] = wrap(value, c.width)
			}
			height = max(height, len(cells[i]))
		}
		item := []string{}
		for line := 0; line < height; line++ {
			values := []string{}
			for i, c := range columns {
				value := ""
				if line < len(cells[i]) {
					value = cells[i][line]
				}
				values = append(values, tableCell(value, c.width))
			}
			marker := "  "
			if r.selected && line == 0 {
				marker = "> "
			}
			item = append(item, marker+strings.Join(values, " │ "))
		}
		for n := 0; n < gap; n++ {
			item = append(item, "")
		}
		items = append(items, item)
	}
	return append(lines, viewport(items, selected, max(1, budget-len(lines)))...)
}

func (m model) resourceTable(budget int) []string {
	var columns []tableColumn
	records := []tableRecord{}
	empty := "Nenhum registro neste filtro."
	switch m.resourceKind() {
	case adaptersResource:
		columns = fitColumns(m.width, []tableColumn{
			{key: "name", title: "NOME"}, {key: "adapter", title: "ADAPTADOR", width: 9},
			{key: "state", title: "ESTADO", width: 9}, {key: "scan", title: "BUSCA", width: 5, priority: 1},
			{key: "active", title: "ATIVO", width: 5, priority: 2}, {key: "mac", title: "MAC", width: 17, priority: 3},
		})
		for _, a := range m.adapterRows() {
			name := a.Name
			if name == "" {
				name = strings.TrimPrefix(a.Path, "/org/bluez/")
			}
			state := "desligado"
			if a.Powered {
				state = "ligado"
			}
			records = append(records, tableRecord{selected: a.Path == m.adapterCursor, values: map[string]string{
				"name": name, "adapter": strings.TrimPrefix(a.Path, "/org/bluez/"), "state": state,
				"scan": yesNo(a.Discovering), "active": yesNo(a.Path == m.adapter), "mac": a.Address,
			}})
		}
	case requestsResource:
		columns = fitColumns(m.width, []tableColumn{
			{key: "id", title: "ID", width: 4, priority: 2}, {key: "name", title: "DISPOSITIVO"},
			{key: "kind", title: "SOLICITAÇÃO", width: 13, priority: 1}, {key: "state", title: "ESTADO", width: 11},
			{key: "expires", title: "EXPIRA", width: 7},
		})
		for _, r := range m.requestRows() {
			name, _ := m.requestIdentity(r)
			expires := "—"
			if r.State == "pending" {
				expires = max(0, time.Until(r.Deadline).Round(time.Second)).String()
			}
			state := requestState(r.State)
			if r.State == "info" {
				state = "Informativo"
			}
			kind := map[string]string{"confirm": "Confirmação", "pin": "PIN", "passkey": "Chave", "authorize": "Pareamento", "service": "Serviço", "display": "Instrução"}[r.Kind]
			records = append(records, tableRecord{selected: r.ID == m.requestID, values: map[string]string{
				"id": fmt.Sprint(r.ID), "name": name, "kind": kind, "state": state, "expires": expires,
			}, wrapName: strings.HasPrefix(name, "Dispositivo sem nome · ")})
		}
	default:
		columns = fitColumns(m.width, []tableColumn{
			{key: "name", title: "NOME"}, {key: "type", title: "TIPO", width: 18, priority: 2},
			{key: "state", title: "CONEXÃO", width: 12}, {key: "paired", title: "PAREADO", width: 7, priority: 1},
			{key: "battery", title: "BATERIA", width: 7}, {key: "mac", title: "MAC", width: 17, priority: 3},
		})
		hasMAC := false
		for _, c := range columns {
			if c.key == "mac" {
				hasMAC = true
			}
		}
		for _, d := range m.rows() {
			state := "desconectado"
			if d.Connected {
				state = "conectado"
			}
			if action := m.busy[d.Path]; action != "" {
				if label := map[string]string{"connect": "conectando", "disconnect": "desconexão…", "pair": "pareando", "cancel": "cancelando", "forget": "esquecendo"}[action]; label != "" {
					state = label
				}
			}
			battery := "—"
			if d.Battery != nil {
				battery = fmt.Sprintf("%d%%", *d.Battery)
			}
			name := d.Label()
			unnamed := name == "Dispositivo sem nome · "+d.Address
			if unnamed && hasMAC {
				name = "Dispositivo sem nome"
			}
			records = append(records, tableRecord{selected: d.Path == m.selected, values: map[string]string{
				"name": name, "type": readableType(d.Type), "state": state, "paired": yesNo(d.Paired), "battery": battery, "mac": d.Address,
			}, wrapName: unnamed})
		}
		if m.resourceKind() == connectionsResource {
			empty = "Nenhuma conexão ativa neste filtro."
		} else {
			empty = "Nenhum dispositivo. s inicia busca."
		}
	}
	return renderTable(columns, records, budget, 0, empty)
}

func (m model) resourceChooser(budget int) []string {
	columns := []tableColumn{{key: "kind", title: "RECURSO", width: 12}, {key: "description", title: "DESCRIÇÃO", width: max(1, m.width-17), wrap: true}}
	records := []tableRecord{}
	for i, r := range resourceMatches(m.input) {
		records = append(records, tableRecord{selected: i == m.commandIndex, values: map[string]string{"kind": string(r.kind), "description": r.description}})
	}
	return renderTable(columns, records, budget, 1, "Recurso desconhecido.")
}

func yesNo(value bool) string {
	if value {
		return "sim"
	}
	return "não"
}
