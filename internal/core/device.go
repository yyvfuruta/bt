package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type Device struct {
	Path           string `json:"path"`
	Adapter        string `json:"adapter"`
	AdapterAddress string `json:"adapter_address,omitempty"`
	Address        string `json:"address"`
	Name           string `json:"name,omitempty"`
	Alias          string `json:"bluez_alias,omitempty"`
	Nickname       string `json:"nickname,omitempty"`
	KnownName      string `json:"known_name,omitempty"`
	Type           string `json:"type"`
	Connected      bool   `json:"connected"`
	Paired         bool   `json:"paired"`
	Battery        *uint8 `json:"battery,omitempty"`
}

func (d Device) MarshalJSON() ([]byte, error) {
	type plain Device
	return json.Marshal(struct {
		plain
		DisplayName string `json:"display_name"`
	}{plain(d), d.Label()})
}

// Clean prevents remote names and persisted data from injecting terminal controls.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, s)
}

func Readable(s, address string) bool {
	s = strings.TrimSpace(Clean(s))
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.ToUpper(s), "-", ""), ":", "")
	mac := strings.ReplaceAll(strings.ToUpper(address), ":", "")
	return s != "" && normalized != mac
}

func (d Device) Label() string {
	for _, s := range []string{d.Nickname, d.Name, d.Alias, d.KnownName} {
		if Readable(s, d.Address) {
			return Clean(s)
		}
	}
	return "Dispositivo sem nome · " + d.Address
}

func Filter(ds []Device, query, state string) []Device {
	out := []Device{}
	query = strings.ToLower(query)
	for _, d := range ds {
		if state == "connected" && !d.Connected || state == "paired" && !d.Paired || state == "unpaired" && d.Paired {
			continue
		}
		if strings.Contains(strings.ToLower(strings.Join([]string{d.Label(), d.Name, d.Alias, d.KnownName, d.Address, d.Type}, " ")), query) {
			out = append(out, d)
		}
	}
	return out
}

// Resolve requires exact names, aliases or MACs; partial matches never connect a device.
func Resolve(ds []Device, query string) (Device, error) {
	matches := []Device{}
	for _, d := range ds {
		for _, s := range []string{d.Address, d.Nickname, d.Name, d.Alias, d.KnownName} {
			if s != "" && strings.EqualFold(s, query) {
				matches = append(matches, d)
				break
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Device{}, fmt.Errorf("dispositivo %q não encontrado; use bt scan ou bt devices", Clean(query))
	}
	details := []string{}
	for _, d := range matches {
		details = append(details, fmt.Sprintf("%s (%s, %s)", d.Label(), d.Address, d.Adapter))
	}
	sort.Strings(details)
	return Device{}, fmt.Errorf("nome ambíguo %q: %s; use o MAC e --adapter", Clean(query), strings.Join(details, "; "))
}

// Stable keeps surviving rows in place and appends new identities deterministically.
func Stable(previous, incoming []Device) []Device {
	byID := map[string]Device{}
	for _, d := range incoming {
		byID[d.Path] = d
	}
	out := []Device{}
	for _, d := range previous {
		if next, ok := byID[d.Path]; ok {
			out = append(out, next)
			delete(byID, d.Path)
		}
	}
	rest := []Device{}
	for _, d := range byID {
		rest = append(rest, d)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Path < rest[j].Path })
	return append(out, rest...)
}
