package bluez

import (
	"context"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func variants(values map[string]any) map[string]dbus.Variant {
	p := map[string]dbus.Variant{}
	for k, v := range values {
		p[k] = dbus.MakeVariant(v)
	}
	return p
}
func TestDecodeAndAdapterSelection(t *testing.T) {
	objects := Objects{
		"/org/bluez/hci1":        {service + ".Adapter1": variants(map[string]any{"Address": "BB", "Alias": "USB", "Powered": false})},
		"/org/bluez/hci0":        {service + ".Adapter1": variants(map[string]any{"Address": "AA", "Alias": "Interno", "Powered": true})},
		"/org/bluez/hci0/dev_AA": {service + ".Device1": variants(map[string]any{"Address": "AA", "Adapter": dbus.ObjectPath("/org/bluez/hci0"), "Name": "Fone", "Class": uint32(4 << 8), "Paired": true, "Connected": true}), service + ".Battery1": variants(map[string]any{"Percentage": uint8(70)})},
		"/org/bluez/hci1/dev_BB": {service + ".Device1": variants(map[string]any{"Address": "BB", "Adapter": dbus.ObjectPath("/org/bluez/hci1")})},
	}
	s := Decode(objects)
	if len(s.Adapters) != 2 || s.Adapters[0].Path != "/org/bluez/hci0" || len(s.Devices) != 2 {
		t.Fatalf("%+v", s)
	}
	d := s.Devices[0]
	if !d.Paired || !d.Connected || d.Type != "áudio/vídeo" || d.Battery == nil || *d.Battery != 70 {
		t.Fatalf("%+v", d)
	}
	if s.Devices[1].Battery != nil || s.Devices[1].Type != "desconhecido" {
		t.Fatal("invented data")
	}
	for _, selector := range []string{"hci1", "bb", "/org/bluez/hci1"} {
		a, err := SelectAdapter(s, selector)
		if err != nil || a.Name != "USB" {
			t.Fatalf("%+v %v", a, err)
		}
	}
	if _, err := SelectAdapter(Snapshot{}, ""); err == nil {
		t.Fatal("missing adapter error")
	}
	if _, err := SelectAdapter(s, "hci2"); err == nil {
		t.Fatal("unknown adapter error")
	}
	if len(ForAdapter(s, "/org/bluez/hci0")) != 1 {
		t.Fatal("adapter filter")
	}
}
func TestFriendlyErrors(t *testing.T) {
	for name, want := range map[string]string{
		"org.bluez.Error.NotReady":                  "rfkill",
		"org.freedesktop.DBus.Error.ServiceUnknown": "systemctl",
		"org.bluez.Error.AuthenticationFailed":      "chave",
		"org.freedesktop.DBus.Error.AccessDenied":   "Polkit",
	} {
		err := Friendly(dbus.NewError(name, []any{"erro"}))
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if !strings.Contains(Friendly(context.DeadlineExceeded).Error(), "tempo limite") {
		t.Fatal("deadline hint")
	}
}
