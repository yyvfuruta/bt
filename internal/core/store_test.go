package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistsMergesAndForgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bt", "devices.json")
	one, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := Load(path)
	d := Device{Adapter: "/org/bluez/hci0", Address: "AA", Name: "Nome conhecido"}
	if err = one.Update([]Device{d}, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	nick := "Meu fone"
	if err = two.Update(nil, &d, &nick, false); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ds := []Device{{Adapter: d.Adapter, Address: d.Address}}
	s.Decorate(ds)
	if ds[0].KnownName != "Nome conhecido" || ds[0].Label() != nick {
		t.Fatalf("lost cache: %+v", ds[0])
	}
	other := []Device{{Adapter: "/org/bluez/hci1", Address: d.Address}}
	s.Decorate(other)
	if other[0].Nickname != "" {
		t.Fatal("cache leaked between adapters")
	}
	nick = ""
	if err = s.Update(nil, &d, &nick, false); err != nil {
		t.Fatal(err)
	}
	s.Decorate(ds)
	if ds[0].Label() != d.Name {
		t.Fatal("nickname removal lost name")
	}
	if err = s.Update(nil, &d, nil, true); err != nil {
		t.Fatal(err)
	}
	s.Decorate(ds)
	if ds[0].KnownName != "" {
		t.Fatal("forget retained name")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", info.Mode())
	}
}
func TestCorruptCacheIsNotSilentlyOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	_ = os.WriteFile(path, []byte("broken"), 0600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("cache changed")
	}
}
