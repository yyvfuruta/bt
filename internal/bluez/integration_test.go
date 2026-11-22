package bluez

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/yyvfuruta/bt/internal/core"
)

// This is a protocol fixture on a private bus, never a replacement for BlueZ in
// the product. No test in this file connects to the real system bus.
func privateBus(t *testing.T) string {
	t.Helper()
	exe, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon não instalado; teste de integração ignorado")
	}
	cmd := exec.Command(exe, "--session", "--nofork", "--print-address=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimSpace(line)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", address)
	return address
}

type fixture struct {
	mu      sync.Mutex
	objects Objects
	owner   string
	agent   dbus.ObjectPath
	calls   []string
}

func (f *fixture) GetManagedObjects() (Objects, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.objects, nil
}
func (f *fixture) RegisterAgent(sender dbus.Sender, path dbus.ObjectPath, capability string) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owner = string(sender)
	f.agent = path
	return nil
}
func (f *fixture) UnregisterAgent(path dbus.ObjectPath) *dbus.Error {
	f.record("unregister")
	return nil
}
func (f *fixture) RequestDefaultAgent(path dbus.ObjectPath) *dbus.Error {
	f.record("default")
	return nil
}
func (f *fixture) record(s string)             { f.mu.Lock(); defer f.mu.Unlock(); f.calls = append(f.calls, s) }
func (f *fixture) StartDiscovery() *dbus.Error { f.record("start"); return nil }
func (f *fixture) StopDiscovery() *dbus.Error  { f.record("stop"); return nil }
func (f *fixture) RemoveDevice(path dbus.ObjectPath) *dbus.Error {
	f.record("forget:" + string(path))
	return nil
}
func (f *fixture) Connect() *dbus.Error       { f.record("connect"); return nil }
func (f *fixture) Disconnect() *dbus.Error    { f.record("disconnect"); return nil }
func (f *fixture) Pair() *dbus.Error          { f.record("pair"); return nil }
func (f *fixture) CancelPairing() *dbus.Error { f.record("cancel"); return nil }
func (f *fixture) Set(iface, name string, value dbus.Variant) *dbus.Error {
	f.record("set:" + iface + "." + name)
	return nil
}
func awaitRequest(t *testing.T, q *core.Requests) core.Request {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, r := range q.Snapshot() {
			if r.State == "pending" {
				return r
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no request received")
	return core.Request{}
}
func TestPrivateDBusProtocol(t *testing.T) {
	addr := privateBus(t)
	server, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err = server.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	f := &fixture{objects: Objects{
		"/org/bluez/hci0":        {service + ".Adapter1": variants(map[string]any{"Address": "00:11:22:33:44:55", "Alias": "Teste", "Powered": true})},
		"/org/bluez/hci0/dev_AA": {service + ".Device1": variants(map[string]any{"Address": "AA:BB:CC:DD:EE:FF", "Name": "Fone", "Adapter": dbus.ObjectPath("/org/bluez/hci0")})},
	}}
	for _, e := range []struct {
		path  dbus.ObjectPath
		iface string
	}{{"/", "org.freedesktop.DBus.ObjectManager"}, {"/org/bluez", service + ".AgentManager1"}, {"/org/bluez/hci0", service + ".Adapter1"}, {"/org/bluez/hci0", props}, {"/org/bluez/hci0/dev_AA", service + ".Device1"}} {
		if err = server.Export(f, e.path, e.iface); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := c.Snapshot(ctx)
	if err != nil || len(s.Devices) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	a := s.Adapters[0]
	d := s.Devices[0]
	if err = c.StartScan(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = c.StartScan(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = c.StopScan(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = c.Power(ctx, a, false); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"connect", "disconnect", "pair", "cancel", "forget"} {
		if err = c.Action(ctx, d, action); err != nil {
			t.Fatal(err)
		}
	}
	q := core.NewRequests(time.Second)
	ready, err := c.RegisterAgent(ctx, q, true, true)
	if !ready || err != nil {
		t.Fatalf("%t %v", ready, err)
	}
	f.mu.Lock()
	owner, path := f.owner, f.agent
	f.mu.Unlock()
	agent := server.Object(owner, path)
	done := make(chan *dbus.Call, 1)
	go func() {
		done <- agent.CallWithContext(ctx, service+".Agent1.RequestConfirmation", 0, dbus.ObjectPath(d.Path), uint32(42))
	}()
	r := awaitRequest(t, q)
	if !strings.Contains(r.Detail, "000042") {
		t.Fatal(r)
	}
	// A snapshot and a display event still complete while a reply is pending.
	if _, err = c.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if err = agent.CallWithContext(ctx, service+".Agent1.DisplayPasskey", 0, dbus.ObjectPath(d.Path), uint32(123456), uint16(2)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("confirmation returned without user consent")
	default:
	}
	if err = q.Respond(r.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if call := <-done; call.Err != nil {
		t.Fatal(call.Err)
	}
	go func() {
		done <- agent.CallWithContext(ctx, service+".Agent1.AuthorizeService", 0, dbus.ObjectPath(d.Path), "test-service")
	}()
	r = awaitRequest(t, q)
	if err = q.Respond(r.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if call := <-done; call.Err == nil || !strings.Contains(call.Err.Error(), "rejeitada") {
		t.Fatalf("rejection: %v", call.Err)
	}
	go func() {
		done <- agent.CallWithContext(ctx, service+".Agent1.RequestAuthorization", 0, dbus.ObjectPath(d.Path))
	}()
	awaitRequest(t, q)
	if err = agent.CallWithContext(ctx, service+".Agent1.Cancel", 0).Err; err != nil {
		t.Fatal(err)
	}
	if call := <-done; call.Err == nil {
		t.Fatal("cancel returned success")
	}
	// Sender spoofing must fail before a notification can be inserted.
	attacker, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer attacker.Close()
	before := len(q.Snapshot())
	err = attacker.Object(owner, path).CallWithContext(ctx, service+".Agent1.RequestAuthorization", 0, dbus.ObjectPath(d.Path)).Err
	if err == nil || len(q.Snapshot()) != before {
		t.Fatalf("spoof not rejected: %v", err)
	}
	if err = server.Emit(dbus.ObjectPath(d.Path), props+".PropertiesChanged", service+".Device1", variants(map[string]any{"Connected": true}), []string{}); err != nil {
		t.Fatal(err)
	}
	received := false
	for !received {
		select {
		case sig := <-c.Signals:
			received = sig != nil && sig.Name == props+".PropertiesChanged"
		case <-ctx.Done():
			t.Fatal("signal missing")
		}
	}
	if err = agent.CallWithContext(ctx, service+".Agent1.Release", 0).Err; err != nil || c.Agent.Active.Load() {
		t.Fatalf("release: %v", err)
	}
	f.mu.Lock()
	calls := strings.Join(f.calls, ",")
	f.mu.Unlock()
	if strings.Count(calls, "start") != 1 || !strings.Contains(calls, "stop") || !strings.Contains(calls, "forget:") {
		t.Fatal(calls)
	}
}
