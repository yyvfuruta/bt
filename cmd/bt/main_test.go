package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	o, err := parse([]string{"connect", "Meu Fone", "--json", "--adapter=hci1", "--timeout", "30s"})
	if err != nil || !o.json || o.adapter != "hci1" || o.timeout != 30*time.Second || o.args[1] != "Meu Fone" {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parse([]string{"alias", "AA", ""})
	if err != nil || len(o.args) != 3 {
		t.Fatalf("empty nickname: %+v %v", o, err)
	}
	for _, args := range [][]string{{"--duration", "0s"}, {"--timeout", "bad"}, {"--adapter"}, {"--filter", "bad"}, {"--unknown"}} {
		if _, err := parse(args); err == nil {
			t.Fatal("expected parse error", args)
		}
	}
}
func TestHelpAndUsageWithoutDBus(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/does/not/exist")
	var b bytes.Buffer
	if err := run(context.Background(), options{args: []string{"--help"}}, &b); err != nil || !strings.Contains(b.String(), "bt scan") {
		t.Fatalf("%s %v", b.String(), err)
	}
	for _, args := range [][]string{{"connect"}, {"devices", "extra"}, {"power", "bad"}, {"unknown"}} {
		err := run(context.Background(), options{args: args}, &b)
		if err == nil || strings.Contains(err.Error(), "D-Bus") {
			t.Fatal("expected usage error before opening bus", args, err)
		}
	}
}
