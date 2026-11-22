package bluez

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/yyvfuruta/bt/internal/core"
)

const agentPath dbus.ObjectPath = "/io/github/yyvfuruta/bt/agent"

type Agent struct {
	Queue       *core.Requests
	Active      atomic.Bool
	owner       dbus.Sender
	interactive bool
}

// RegisterAgent exports only Agent1 methods. Every call checks the BlueZ unique
// bus owner, so another local D-Bus client cannot forge authorization requests.
func (c *Client) RegisterAgent(ctx context.Context, queue *core.Requests, interactive, makeDefault bool) (bool, error) {
	var owner string
	if err := c.Conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, service).Store(&owner); err != nil {
		return false, Friendly(err)
	}
	a := &Agent{Queue: queue, owner: dbus.Sender(owner), interactive: interactive}
	a.Active.Store(true)
	if err := c.Conn.Export(a, agentPath, service+".Agent1"); err != nil {
		return false, err
	}
	if err := c.Conn.Export(introspect.NewIntrospectable(&introspect.Node{Interfaces: []introspect.Interface{{Name: service + ".Agent1", Methods: introspect.Methods(a)}}}), agentPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return false, err
	}
	if err := c.Call(ctx, "/org/bluez", service+".AgentManager1.RegisterAgent", agentPath, "KeyboardDisplay"); err != nil {
		return false, err
	}
	c.Agent = a
	if makeDefault {
		return true, c.Call(ctx, "/org/bluez", service+".AgentManager1.RequestDefaultAgent", agentPath)
	}
	return true, nil
}

// Keep the agent immutable after registration; D-Bus dispatches methods concurrently.
func (a *Agent) verify(sender dbus.Sender) *dbus.Error {
	if sender != a.owner {
		return dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{"somente BlueZ pode chamar o agente"})
	}
	return nil
}
func (a *Agent) ask(sender dbus.Sender, device dbus.ObjectPath, kind, detail string) (string, *dbus.Error) {
	if err := a.verify(sender); err != nil {
		return "", err
	}
	if !a.interactive {
		return "", dbus.NewError("org.bluez.Error.Rejected", []any{"resposta explícita necessária; abra bt e pareie/conecte pela interface"})
	}
	if !a.Active.Load() {
		return "", dbus.NewError("org.bluez.Error.Canceled", []any{"agente inativo"})
	}
	answer := a.Queue.Ask(string(device), kind, detail)
	switch answer.State {
	case "accepted":
		return answer.Value, nil
	case "rejected":
		return "", dbus.NewError("org.bluez.Error.Rejected", []any{"solicitação rejeitada"})
	default:
		return "", dbus.NewError("org.bluez.Error.Canceled", []any{answer.State})
	}
}
func (a *Agent) RequestPinCode(sender dbus.Sender, device dbus.ObjectPath) (string, *dbus.Error) {
	return a.ask(sender, device, "pin", "Informe o PIN de 1 a 16 bytes")
}
func (a *Agent) RequestPasskey(sender dbus.Sender, device dbus.ObjectPath) (uint32, *dbus.Error) {
	v, err := a.ask(sender, device, "passkey", "Informe a chave de até 6 dígitos")
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseUint(v, 10, 32)
	return uint32(n), nil
}
func (a *Agent) RequestConfirmation(sender dbus.Sender, device dbus.ObjectPath, passkey uint32) *dbus.Error {
	_, err := a.ask(sender, device, "confirm", fmt.Sprintf("Confirme a chave %06d nos dois dispositivos", passkey))
	return err
}
func (a *Agent) RequestAuthorization(sender dbus.Sender, device dbus.ObjectPath) *dbus.Error {
	_, err := a.ask(sender, device, "authorize", "Autorizar pareamento solicitado pelo dispositivo?")
	return err
}
func (a *Agent) AuthorizeService(sender dbus.Sender, device dbus.ObjectPath, uuid string) *dbus.Error {
	_, err := a.ask(sender, device, "service", "Autorizar serviço "+core.Clean(uuid)+"?")
	return err
}
func (a *Agent) DisplayPinCode(sender dbus.Sender, device dbus.ObjectPath, pin string) *dbus.Error {
	if err := a.verify(sender); err != nil {
		return err
	}
	a.Queue.Info(string(device), "Digite no dispositivo remoto o PIN "+core.Clean(pin))
	return nil
}
func (a *Agent) DisplayPasskey(sender dbus.Sender, device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	if err := a.verify(sender); err != nil {
		return err
	}
	a.Queue.Info(string(device), fmt.Sprintf("Digite no dispositivo remoto %06d (%d dígitos inseridos)", passkey, entered))
	return nil
}
func (a *Agent) Cancel(sender dbus.Sender) *dbus.Error {
	if err := a.verify(sender); err != nil {
		return err
	}
	a.Queue.Cancel()
	return nil
}
func (a *Agent) Release(sender dbus.Sender) *dbus.Error {
	if err := a.verify(sender); err != nil {
		return err
	}
	a.Active.Store(false)
	a.Queue.Close()
	return nil
}
