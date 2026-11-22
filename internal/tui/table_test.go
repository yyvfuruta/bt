package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/yyvfuruta/bt/internal/core"
)

func TestColumnsAreAlignedAndHeadersNeverTruncate(t *testing.T) {
	for width := 48; width <= 140; width++ {
		m := layoutModel(t)
		m.width = width
		m.height = 24
		m.devices[0].Name = "Fone 🎧 東京"
		for _, kind := range []resourceKind{devicesResource, connectionsResource, adaptersResource, requestsResource} {
			m.switchResource(kind)
			view := assertFits(t, m)
			var header []string
			for _, line := range strings.Split(view, "\n") {
				if strings.Contains(line, "NOME") || strings.Contains(line, "DISPOSITIVO") {
					header = strings.Split(line, "│")
					break
				}
			}
			if len(header) < 4 {
				t.Fatalf("width %d: no column headers for %s:\n%s", width, kind, view)
			}
			for _, heading := range header {
				if strings.Contains(heading, "…") {
					t.Fatalf("truncated header: %s", view)
				}
			}
			// Data separators must occupy the same terminal cells as the headers,
			// including Unicode names whose byte/rune counts differ from widths.
			positions := separatorPositions(strings.Join(header, "│"))
			for _, line := range strings.Split(view, "\n") {
				if strings.HasPrefix(line, "│ > ") {
					got := separatorPositions(line)
					if len(got) != len(positions) {
						t.Fatal("separator count changed", line)
					}
					for i := range got {
						if got[i] != positions[i] {
							t.Fatalf("misaligned at width %d: %v vs %v", width, got, positions)
						}
					}
				}
			}
		}
	}
}

func separatorPositions(line string) []int {
	positions := []int{}
	prefix := ""
	for _, r := range line {
		if r == '│' {
			positions = append(positions, ansi.StringWidth(prefix))
		}
		prefix += string(r)
	}
	return positions
}

func TestUnknownDevicesKeepFullDistinctIdentifiers(t *testing.T) {
	m := layoutModel(t)
	m.devices = []core.Device{
		{Path: "one", Adapter: m.adapter, Address: "02:00:00:00:00:01"},
		{Path: "two", Adapter: m.adapter, Address: "02:00:00:00:00:02"},
	}
	for _, width := range []int{48, 57, 60, 79, 80, 99, 100, 140} {
		m.width = width
		m.height = 30
		m.selected = "one"
		view := assertFits(t, m)
		for _, d := range m.devices {
			if !strings.Contains(view, d.Address) {
				t.Fatalf("identifier lost at width %d:\n%s", width, view)
			}
		}
	}
}

func TestChooserSeparatesResourceAndDescriptionAndScrolls(t *testing.T) {
	m := layoutModel(t)
	m.width, m.height = 48, 18
	m.mode = "command"
	m.input = ""
	view := assertFits(t, m)
	for _, text := range []string{"RECURSO", "DESCRIÇÃO", "┼", "devices"} {
		if !strings.Contains(view, text) {
			t.Fatal("chooser lacks separation", text, view)
		}
	}
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}
	view = assertFits(t, m)
	if !strings.Contains(view, "> requests") {
		t.Fatalf("selected resource hidden below viewport:\n%s", view)
	}
	// Inspect the unframed table so viewport padding cannot mask missing gaps.
	content := m
	content.width -= 4
	lines := content.resourceChooser(100)
	previous := -1
	for i, line := range lines {
		if strings.Contains(line, "devices ") || strings.Contains(line, "connections ") || strings.Contains(line, "adapters ") || strings.Contains(line, "requests ") {
			if previous >= 0 && lines[i-1] != "" {
				t.Fatal("entries have no breathing room", lines)
			}
			previous = i
		}
	}
}
