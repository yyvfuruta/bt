package tui

import (
	"fmt"
	"strings"

	"github.com/yyvfuruta/bt/internal/bluez"
	"github.com/yyvfuruta/bt/internal/core"
)

type resourceKind string

const (
	devicesResource     resourceKind = "devices"
	connectionsResource resourceKind = "connections"
	adaptersResource    resourceKind = "adapters"
	requestsResource    resourceKind = "requests"
)

// Resources are local views over BlueZ, not Kubernetes objects. A connection
// represents Device1.Connected, not an invented per-profile connection object.
type resourceDefinition struct {
	kind               resourceKind
	label, description string
	aliases            []string
}

var resources = []resourceDefinition{
	{devicesResource, "Dispositivos", "Dispositivos conhecidos pelo BlueZ", []string{"dev", "device", "dispositivos"}},
	{connectionsResource, "Conexões", "Links Bluetooth conectados neste momento", []string{"conn", "connection", "conexoes", "conexões"}},
	{adaptersResource, "Adaptadores", "Controladores Bluetooth e alimentação", []string{"ad", "adapter", "adaptadores"}},
	{requestsResource, "Pedidos", "Pareamento, autorização e instruções", []string{"req", "request", "pedidos"}},
}

func resolveResource(name string) (resourceKind, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, r := range resources {
		if string(r.kind) == name {
			return r.kind, nil
		}
		for _, alias := range r.aliases {
			if alias == name {
				return r.kind, nil
			}
		}
	}
	return "", fmt.Errorf("recurso %q desconhecido; use devices, connections, adapters ou requests", core.Clean(name))
}
func resourceMatches(query string) []resourceDefinition {
	query = strings.ToLower(strings.TrimSpace(query))
	query, _, _ = strings.Cut(query, " ")
	out := []resourceDefinition{}
	for _, r := range resources {
		words := append([]string{string(r.kind), r.label}, r.aliases...)
		for _, word := range words {
			if strings.Contains(strings.ToLower(word), query) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}
func (m model) resourceKind() resourceKind {
	if m.focusRequests {
		return requestsResource
	}
	if m.resource == "" {
		return devicesResource
	}
	return m.resource
}
func (m *model) switchResource(kind resourceKind) {
	if m.queries == nil {
		m.queries = map[resourceKind]string{}
	}
	m.queries[m.resourceKind()] = m.query
	m.resource = kind
	m.focusRequests = kind == requestsResource
	m.showDetails = false
	m.detailScroll = 0
	m.query = m.queries[kind]
	m.selectFirst()
}
func (m model) adapterRows() []bluez.Adapter {
	out := []bluez.Adapter{}
	for _, a := range m.adapters {
		state := "desligado"
		if a.Powered {
			state = "ligado"
		}
		text := strings.Join([]string{a.Name, a.Path, a.Address, state}, " ")
		if strings.Contains(strings.ToLower(text), strings.ToLower(m.query)) {
			out = append(out, a)
		}
	}
	return out
}
func (m model) requestRows() []core.Request {
	out := []core.Request{}
	for _, r := range m.requests.Snapshot() {
		text := strings.Join([]string{r.Device, r.Detail, r.Kind, r.State, requestState(r.State)}, " ")
		for _, d := range m.devices {
			if d.Path == r.Device {
				text += " " + d.Label() + " " + d.Address
				break
			}
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(m.query)) {
			out = append(out, r)
		}
	}
	return out
}
func (m model) request() (core.Request, bool) {
	for _, r := range m.requestRows() {
		if r.ID == m.requestID {
			return r, true
		}
	}
	return core.Request{}, false
}
func (m model) selectedAdapter() (bluez.Adapter, bool) {
	if m.resourceKind() != adaptersResource {
		return m.adapterValue()
	}
	for _, a := range m.adapterRows() {
		if a.Path == m.adapterCursor {
			return a, true
		}
	}
	return bluez.Adapter{}, false
}
func (m *model) openResourceCommand() error {
	input := strings.TrimSpace(m.input)
	name, query, hasQuery := strings.Cut(input, " ")
	kind, err := resolveResource(name)
	if err != nil && !hasQuery {
		matches := resourceMatches(name)
		if len(matches) > 0 {
			index := min(m.commandIndex, len(matches)-1)
			kind = matches[index].kind
			err = nil
		}
	}
	if err != nil {
		return err
	}
	m.switchResource(kind)
	if hasQuery {
		m.query = strings.TrimPrefix(strings.TrimSpace(query), "/")
		m.selectFirst()
	}
	return nil
}
