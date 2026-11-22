package core

import (
	"strings"
	"testing"
)

func TestLabels(t *testing.T) {
	for _, tc := range []struct {
		d    Device
		want string
	}{
		{Device{Address: "AA:BB:CC:DD:EE:FF"}, "Dispositivo sem nome · AA:BB:CC:DD:EE:FF"},
		{Device{Address: "AA:BB:CC:DD:EE:FF", Alias: "aa-bb-cc-dd-ee-ff", KnownName: "Fone"}, "Fone"},
		{Device{Name: "Nome atual", KnownName: "Antigo"}, "Nome atual"},
		{Device{Name: "Nome", Nickname: "Meu fone"}, "Meu fone"},
		{Device{Name: "Fone\n\x1b\u202e"}, "Fone"},
	} {
		if got := tc.d.Label(); got != tc.want {
			t.Errorf("Label = %q, want %q", got, tc.want)
		}
	}
}
func TestResolveAmbiguity(t *testing.T) {
	ds := []Device{{Path: "a", Adapter: "hci0", Name: "Fone", Address: "AA"}, {Path: "b", Adapter: "hci1", Nickname: "Fone", Address: "BB"}}
	if _, err := Resolve(ds, "fone"); err == nil || !strings.Contains(err.Error(), "ambíguo") || !strings.Contains(err.Error(), "BB") {
		t.Fatalf("ambiguity: %v", err)
	}
	if d, err := Resolve(ds, "aa"); err != nil || d.Path != "a" {
		t.Fatalf("MAC resolution: %+v %v", d, err)
	}
	if _, err := Resolve(ds, "Fon"); err == nil {
		t.Fatal("partial match must not resolve")
	}
}
func TestStableAndFilter(t *testing.T) {
	previous := []Device{{Path: "b", Name: "Z"}, {Path: "c", Name: "C"}}
	next := []Device{{Path: "a", Name: "A"}, {Path: "c", Name: "Changed", Paired: true}, {Path: "b", Name: "Renamed", Connected: true}}
	ds := Stable(previous, next)
	if ds[0].Path != "b" || ds[1].Path != "c" || ds[2].Path != "a" || ds[0].Name != "Renamed" {
		t.Fatalf("unstable: %+v", ds)
	}
	ds = Filter(ds, "name", "connected")
	if len(ds) != 1 || ds[0].Path != "b" {
		t.Fatalf("filter: %+v", ds)
	}
	ds = Stable(previous, next[:2])
	if len(ds) != 2 || ds[0].Path != "c" {
		t.Fatalf("removed identity: %+v", ds)
	}
}
