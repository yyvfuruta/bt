//go:build linux

package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type terminalOutput struct {
	mu      sync.Mutex
	data    bytes.Buffer
	changed chan struct{}
}

func (o *terminalOutput) read(master *os.File) {
	buf := make([]byte, 8192)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			o.mu.Lock()
			o.data.Write(buf[:n])
			o.mu.Unlock()
			select {
			case o.changed <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

func (o *terminalOutput) wait(t *testing.T, text string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		o.mu.Lock()
		found := strings.Contains(o.data.String(), text)
		o.mu.Unlock()
		if found {
			return
		}
		select {
		case <-o.changed:
		case <-timer.C:
			o.mu.Lock()
			output := o.data.String()
			o.mu.Unlock()
			t.Fatalf("terminal não exibiu %q: %q", text, output)
		}
	}
}

// Opt-in because it starts an actual CLI in a pseudoterminal. It only uses a
// private D-Bus with no BlueZ and never touches the system Bluetooth service.
func TestTerminalRestoration(t *testing.T) {
	binary := os.Getenv("BT_TEST_BINARY")
	if binary == "" {
		t.Skip("execute make terminal-test para testar o binário em um pseudoterminal")
	}
	exe, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Fatal("dbus-daemon é necessário para o teste de terminal")
	}
	bus := exec.Command(exe, "--session", "--nofork", "--print-address=1")
	pipe, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = bus.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bus.Process.Kill(); _ = bus.Wait() }()
	address, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	address = strings.TrimSpace(address)
	for _, tc := range []struct {
		how        string
		cols, rows uint16
	}{{"q", 48, 18}, {"ctrl-c", 80, 24}, {"sigterm", 100, 30}} {
		t.Run(tc.how, func(t *testing.T) {
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			fd := int(master.Fd())
			if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			if err = unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: tc.rows, Col: tc.cols}); err != nil {
				t.Fatal(err)
			}
			before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			cmd.Env = append(os.Environ(), "DBUS_SYSTEM_BUS_ADDRESS="+address, "XDG_CONFIG_HOME="+t.TempDir(), "TERM=xterm-256color")
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer func() { _ = cmd.Process.Kill() }()
			output := &terminalOutput{changed: make(chan struct{}, 1)}
			go output.read(master)
			output.wait(t, "Tab pedidos")
			raw, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if raw.Lflag&unix.ICANON != 0 {
				t.Fatal("interface não entrou em modo raw")
			}
			switch tc.how {
			case "sigterm":
				err = cmd.Process.Signal(syscall.SIGTERM)
			case "q":
				_, err = master.Write([]byte("q"))
			default:
				_, err = master.Write([]byte{3})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("interface não encerrou")
			}
			after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("terminal não restaurado: antes=%+v depois=%+v", before, after)
			}
			output.wait(t, "\x1b[?1049h")
			output.wait(t, "\x1b[?1049l")
			output.wait(t, "\x1b[?25h")
		})
	}
}
