package bluez

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/yyvfuruta/bt/internal/core"
)

const service = "org.bluez"
const props = "org.freedesktop.DBus.Properties"

type Objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
type Adapter struct {
	Path        string `json:"path"`
	Address     string `json:"address"`
	Name        string `json:"name"`
	Powered     bool   `json:"powered"`
	Discovering bool   `json:"discovering"`
}
type Snapshot struct {
	Adapters []Adapter     `json:"adapters"`
	Devices  []core.Device `json:"devices"`
}
type Client struct {
	Conn    *dbus.Conn
	Signals chan *dbus.Signal
	mu      sync.Mutex
	scans   map[string]bool
	pairs   map[string]bool
	Agent   *Agent
}

func Open() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("D-Bus do sistema indisponível: %w; verifique dbus.service", err)
	}
	c := &Client{Conn: conn, Signals: make(chan *dbus.Signal, 128), scans: map[string]bool{}, pairs: map[string]bool{}}
	conn.Signal(c.Signals)
	rules := [][]dbus.MatchOption{
		{dbus.WithMatchSender(service), dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager")},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(props)},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, service)},
	}
	for _, r := range rules {
		if err = conn.AddMatchSignal(r...); err != nil {
			conn.Close()
			return nil, Friendly(err)
		}
	}
	return c, nil
}
func (c *Client) Call(ctx context.Context, path, method string, args ...any) error {
	return Friendly(c.Conn.Object(service, dbus.ObjectPath(path)).CallWithContext(ctx, method, 0, args...).Err)
}
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var objects Objects
	err := c.Conn.Object(service, "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return Snapshot{}, Friendly(err)
	}
	return Decode(objects), nil
}
func str(p map[string]dbus.Variant, k string) string   { s, _ := p[k].Value().(string); return s }
func boolean(p map[string]dbus.Variant, k string) bool { b, _ := p[k].Value().(bool); return b }
func Decode(objects Objects) Snapshot {
	s := Snapshot{Adapters: []Adapter{}, Devices: []core.Device{}}
	for path, ifaces := range objects {
		if p, ok := ifaces[service+".Adapter1"]; ok {
			s.Adapters = append(s.Adapters, Adapter{string(path), str(p, "Address"), str(p, "Alias"), boolean(p, "Powered"), boolean(p, "Discovering")})
		}
		if p, ok := ifaces[service+".Device1"]; ok {
			adapter, _ := p["Adapter"].Value().(dbus.ObjectPath)
			d := core.Device{Path: string(path), Adapter: string(adapter), Address: str(p, "Address"), Name: str(p, "Name"), Alias: str(p, "Alias"), Type: deviceType(p), Connected: boolean(p, "Connected"), Paired: boolean(p, "Paired")}
			d.AdapterAddress = str(objects[adapter][service+".Adapter1"], "Address")
			if b, ok := ifaces[service+".Battery1"]["Percentage"].Value().(uint8); ok {
				d.Battery = &b
			}
			s.Devices = append(s.Devices, d)
		}
	}
	sort.Slice(s.Adapters, func(i, j int) bool { return s.Adapters[i].Path < s.Adapters[j].Path })
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].Path < s.Devices[j].Path })
	return s
}
func deviceType(p map[string]dbus.Variant) string {
	if icon := str(p, "Icon"); icon != "" {
		return icon
	}
	if class, ok := p["Class"].Value().(uint32); ok {
		switch (class >> 8) & 0x1f {
		case 1:
			return "computador"
		case 2:
			return "telefone"
		case 4:
			return "áudio/vídeo"
		case 5:
			return "periférico"
		case 6:
			return "imagem"
		case 7:
			return "vestível"
		case 8:
			return "brinquedo"
		case 9:
			return "saúde"
		}
	}
	if appearance, ok := p["Appearance"].Value().(uint16); ok {
		switch appearance >> 6 {
		case 1:
			return "telefone"
		case 2:
			return "computador"
		case 3:
			return "relógio"
		case 15:
			return "controle/periférico"
		}
	}
	return "desconhecido"
}
func SelectAdapter(s Snapshot, selector string) (Adapter, error) {
	if len(s.Adapters) == 0 {
		return Adapter{}, fmt.Errorf("nenhum adaptador Bluetooth; conecte um adaptador e verifique drivers e rfkill list")
	}
	if selector == "" {
		return s.Adapters[0], nil
	}
	for _, a := range s.Adapters {
		if a.Path == selector || strings.TrimPrefix(a.Path, "/org/bluez/") == selector || strings.EqualFold(a.Address, selector) {
			return a, nil
		}
	}
	return Adapter{}, fmt.Errorf("adaptador %q não encontrado; use bt status", core.Clean(selector))
}
func ForAdapter(s Snapshot, adapter string) []core.Device {
	ds := []core.Device{}
	for _, d := range s.Devices {
		if adapter == "" || d.Adapter == adapter {
			ds = append(ds, d)
		}
	}
	return ds
}
func (c *Client) Power(ctx context.Context, a Adapter, on bool) error {
	return c.Call(ctx, a.Path, props+".Set", service+".Adapter1", "Powered", dbus.MakeVariant(on))
}
func (c *Client) StartScan(ctx context.Context, a Adapter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scans[a.Path] {
		return nil
	}
	if !a.Powered {
		return fmt.Errorf("Bluetooth desligado em %s; use bt power on --adapter %s", a.Path, strings.TrimPrefix(a.Path, "/org/bluez/"))
	}
	if err := c.Call(ctx, a.Path, service+".Adapter1.StartDiscovery"); err != nil {
		return err
	}
	c.scans[a.Path] = true
	return nil
}
func (c *Client) StopScan(ctx context.Context, a Adapter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.scans[a.Path] {
		return nil
	}
	err := c.Call(ctx, a.Path, service+".Adapter1.StopDiscovery")
	if err == nil {
		delete(c.scans, a.Path)
	}
	return err
}
func (c *Client) Scanning(path string) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.scans[path] }
func (c *Client) ServiceLost() {
	c.mu.Lock()
	c.scans = map[string]bool{}
	c.mu.Unlock()
	if c.Agent != nil {
		c.Agent.Active.Store(false)
		c.Agent.Queue.Close()
	}
}
func (c *Client) Action(ctx context.Context, d core.Device, action string) error {
	switch action {
	case "connect":
		return c.Call(ctx, d.Path, service+".Device1.Connect")
	case "disconnect":
		return c.Call(ctx, d.Path, service+".Device1.Disconnect")
	case "pair":
		c.mu.Lock()
		c.pairs[d.Path] = true
		c.mu.Unlock()
		defer func() { c.mu.Lock(); delete(c.pairs, d.Path); c.mu.Unlock() }()
		err := c.Call(ctx, d.Path, service+".Device1.Pair")
		if ctx.Err() != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.Call(cleanup, d.Path, service+".Device1.CancelPairing")
			cancel()
		}
		return err
	case "cancel":
		return c.Call(ctx, d.Path, service+".Device1.CancelPairing")
	case "forget":
		return c.Call(ctx, d.Adapter, service+".Adapter1.RemoveDevice", dbus.ObjectPath(d.Path))
	}
	return fmt.Errorf("ação desconhecida: %s", action)
}
func (c *Client) Close() {
	if c.Agent != nil {
		c.Agent.Active.Store(false)
		c.Agent.Queue.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c.mu.Lock()
	paths := []string{}
	for p := range c.scans {
		paths = append(paths, p)
	}
	pairs := []string{}
	for p := range c.pairs {
		pairs = append(pairs, p)
	}
	c.mu.Unlock()
	for _, p := range pairs {
		_ = c.Call(ctx, p, service+".Device1.CancelPairing")
	}
	for _, p := range paths {
		_ = c.StopScan(ctx, Adapter{Path: p})
	}
	if c.Agent != nil {
		_ = c.Call(ctx, "/org/bluez", service+".AgentManager1.UnregisterAgent", agentPath)
	}
	c.Conn.RemoveSignal(c.Signals)
	_ = c.Conn.Close()
}
func Friendly(err error) error {
	if err == nil {
		return nil
	}
	var e dbus.Error
	isDBus := errors.As(err, &e)
	var pointer *dbus.Error
	if !isDBus && errors.As(err, &pointer) {
		e = *pointer
		isDBus = true
	}
	if isDBus {
		hint := "verifique bt status e tente novamente"
		switch e.Name {
		case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner":
			hint = "BlueZ indisponível; verifique systemctl status bluetooth"
		case "org.bluez.Error.NotReady":
			hint = "ligue o Bluetooth com bt power on; verifique rfkill list"
		case "org.bluez.Error.AuthenticationRejected", "org.bluez.Error.Rejected":
			hint = "solicitação rejeitada; tente parear novamente pela interface bt"
		case "org.bluez.Error.AuthenticationTimeout":
			hint = "pareamento expirou; coloque o dispositivo em modo de pareamento e tente novamente"
		case "org.bluez.Error.AuthenticationFailed":
			hint = "confira a chave nos dois dispositivos; tente esquecer e parear novamente"
		case "org.bluez.Error.InProgress":
			hint = "operação em andamento; aguarde ou cancele o pareamento"
		case "org.bluez.Error.NotAuthorized", "org.freedesktop.DBus.Error.AccessDenied":
			hint = "permissão negada; verifique a política D-Bus/Polkit e a sessão do usuário"
		case "org.bluez.Error.AlreadyConnected":
			hint = "dispositivo já conectado"
		case "org.bluez.Error.NotConnected":
			hint = "dispositivo já desconectado"
		case "org.freedesktop.DBus.Error.UnknownObject":
			hint = "dispositivo ou adaptador removido; atualize a lista ou execute bt scan"
		}
		return fmt.Errorf("%s: %s (%s)", hint, core.Clean(e.Error()), e.Name)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("tempo limite excedido; verifique o dispositivo e tente novamente")
	}
	return err
}
