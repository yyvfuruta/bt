package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

type Entry struct {
	Name     string `json:"name,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}
type Store struct {
	Path    string
	Entries map[string]Entry
}

func Load(path string) (*Store, error) {
	if path == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(root, "bt", "devices.json")
	}
	s := &Store{Path: path, Entries: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.Entries); err != nil {
		return nil, fmt.Errorf("cache de nomes inválido em %s: %w; corrija ou renomeie o arquivo", path, err)
	}
	if s.Entries == nil {
		s.Entries = map[string]Entry{}
	}
	return s, nil
}

func key(d Device) string {
	adapter := d.AdapterAddress
	if adapter == "" {
		adapter = d.Adapter
	}
	return strings.ToUpper(adapter) + "/" + strings.ToUpper(d.Address)
}

func (s *Store) Decorate(ds []Device) {
	for i := range ds {
		e := s.Entries[key(ds[i])]
		ds[i].KnownName = e.Name
		ds[i].Nickname = e.Nickname
	}
}

// Update locks across CLI processes, merges from disk and atomically replaces the file.
func (s *Store) Update(ds []Device, target *Device, nickname *string, forget bool) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	fresh, err := Load(s.Path)
	if err != nil {
		return err
	}
	before := make(map[string]Entry, len(fresh.Entries))
	for k, e := range fresh.Entries {
		before[k] = e
	}
	for _, d := range ds {
		e := fresh.Entries[key(d)]
		if Readable(d.Name, d.Address) {
			e.Name = Clean(d.Name)
		} else if Readable(d.Alias, d.Address) {
			e.Name = Clean(d.Alias)
		}
		if e.Name != "" || e.Nickname != "" {
			fresh.Entries[key(d)] = e
		}
	}
	if target != nil {
		k := key(*target)
		if forget {
			delete(fresh.Entries, k)
		} else if nickname != nil {
			e := fresh.Entries[k]
			e.Nickname = strings.TrimSpace(Clean(*nickname))
			fresh.Entries[k] = e
		}
	}
	if reflect.DeepEqual(before, fresh.Entries) {
		s.Entries = fresh.Entries
		return nil
	}
	b, err := json.MarshalIndent(fresh.Entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".devices-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), s.Path); err != nil {
		return err
	}
	s.Entries = fresh.Entries
	return nil
}
