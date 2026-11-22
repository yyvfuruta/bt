package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/yyvfuruta/bt/internal/bluez"
	"github.com/yyvfuruta/bt/internal/core"
	"github.com/yyvfuruta/bt/internal/tui"
)

const help = `bt — Bluetooth para Linux via BlueZ/D-Bus

Uso:
  bt                               Abre a interface interativa
  bt scan [--duration 10s]          Busca por tempo limitado e lista dispositivos
  bt devices                       Lista dispositivos conhecidos pelo BlueZ
  bt status                        Mostra adaptadores e dispositivos
  bt connect NOME|APELIDO|MAC
  bt disconnect NOME|APELIDO|MAC
  bt pair NOME|APELIDO|MAC          Se exigir resposta, use a interface bt
  bt cancel NOME|APELIDO|MAC        Cancela pareamento
  bt forget NOME|APELIDO|MAC        Remove pareamento e nome/apelido local
  bt alias NOME|APELIDO|MAC APELIDO  Salva apelido ("" remove)
  bt power on|off

Opções (antes ou depois do comando):
  --json                    Saída JSON; erros em JSON no stderr
  --adapter hci0|MAC|CAMINHO Seleciona adaptador
  --filter all|connected|paired|unpaired
  --search TEXTO            Filtra lista por nome, apelido, MAC ou tipo
  --duration 10s            Duração da busca
  --timeout 2m              Tempo limite dos comandos D-Bus
  -h, --help                Ajuda
  --version                 Versão

Nomes são comparados por igualdade sem distinguir maiúsculas. Nomes ambíguos
exigem MAC e, se necessário, --adapter. Não há aceitação automática de pedidos.
`

type options struct {
	json                    bool
	adapter, filter, search string
	duration, timeout       time.Duration
	args                    []string
}

func parse(args []string) (options, error) {
	o := options{filter: "all", duration: 10 * time.Second, timeout: 2 * time.Minute}
	positional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if positional {
			o.args = append(o.args, arg)
			continue
		}
		if arg == "--" {
			positional = true
			continue
		}
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--json":
			if hasValue {
				return o, fmt.Errorf("--json não recebe valor")
			}
			o.json = true
		case "--help", "-h", "--version":
			o.args = []string{key}
			return o, nil
		case "--adapter", "--filter", "--search", "--duration", "--timeout":
			if !hasValue {
				i++
				if i >= len(args) {
					return o, fmt.Errorf("%s exige valor", key)
				}
				value = args[i]
			}
			switch key {
			case "--adapter":
				o.adapter = value
			case "--filter":
				o.filter = value
			case "--search":
				o.search = value
			default:
				d, err := time.ParseDuration(value)
				if err != nil || d <= 0 {
					return o, fmt.Errorf("%s exige duração positiva, por exemplo 10s", key)
				}
				if key == "--duration" {
					o.duration = d
				} else {
					o.timeout = d
				}
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("opção desconhecida: %s", core.Clean(arg))
			}
			o.args = append(o.args, arg)
		}
	}
	switch o.filter {
	case "all", "connected", "paired", "unpaired":
	default:
		return o, fmt.Errorf("filtro inválido: use all, connected, paired ou unpaired")
	}
	return o, nil
}

func main() {
	o, err := parse(os.Args[1:])
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err == nil {
		err = run(ctx, o, os.Stdout)
	}
	if err != nil {
		if o.json {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": core.Clean(err.Error())})
		} else {
			fmt.Fprintln(os.Stderr, "bt:", core.Clean(err.Error()))
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, o options, out io.Writer) error {
	command := ""
	if len(o.args) > 0 {
		command = o.args[0]
	}
	switch command {
	case "--help", "-h", "help":
		_, err := fmt.Fprint(out, help)
		return err
	case "--version":
		_, err := fmt.Fprintln(out, "bt 0.1.0")
		return err
	case "", "scan", "devices", "status":
		if len(o.args) > 1 {
			return fmt.Errorf("%s não recebe argumentos posicionais", command)
		}
	case "connect", "disconnect", "pair", "forget", "cancel":
		if len(o.args) != 2 {
			return fmt.Errorf("uso: bt %s NOME|APELIDO|MAC", command)
		}
	case "alias":
		if len(o.args) != 3 {
			return fmt.Errorf("uso: bt alias NOME|APELIDO|MAC APELIDO")
		}
	case "power":
		if len(o.args) != 2 || (o.args[1] != "on" && o.args[1] != "off") {
			return fmt.Errorf("uso: bt power on|off [--adapter hci0]")
		}
	default:
		return fmt.Errorf("comando desconhecido: %s; use bt --help", core.Clean(command))
	}
	if command == "" && o.json {
		return fmt.Errorf("use --json com um comando, por exemplo bt devices --json")
	}
	store, err := core.Load("")
	if err != nil {
		return err
	}
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()
	if command == "" {
		// Validate a selector, but let the TUI explain a missing/offline service.
		adapter := ""
		if o.adapter != "" {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			s, e := c.Snapshot(checkCtx)
			cancel()
			if e != nil {
				return e
			}
			a, e := bluez.SelectAdapter(s, o.adapter)
			if e != nil {
				return e
			}
			adapter = a.Path
		}
		return tui.Run(ctx, c, store, tui.Options{Adapter: adapter, Query: o.search, Filter: o.filter})
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	s, err := c.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err = store.Update(s.Devices, nil, nil, false); err != nil {
		return fmt.Errorf("salvar nomes: %w", err)
	}
	store.Decorate(s.Devices)
	adapter := ""
	if o.adapter != "" {
		a, e := bluez.SelectAdapter(s, o.adapter)
		if e != nil {
			return e
		}
		adapter = a.Path
	}
	emit := func(v any) error { enc := json.NewEncoder(out); enc.SetIndent("", "  "); return enc.Encode(v) }
	switch command {
	case "status":
		if o.json {
			filtered := bluez.Snapshot{Adapters: []bluez.Adapter{}, Devices: core.Filter(bluez.ForAdapter(s, adapter), o.search, o.filter)}
			for _, a := range s.Adapters {
				if adapter == "" || a.Path == adapter {
					filtered.Adapters = append(filtered.Adapters, a)
				}
			}
			return emit(filtered)
		}
		if len(s.Adapters) == 0 {
			_, err = fmt.Fprintln(out, "BlueZ disponível; nenhum adaptador. Conecte um adaptador e verifique drivers e rfkill list.")
			return err
		}
		for _, a := range s.Adapters {
			if adapter != "" && a.Path != adapter {
				continue
			}
			fmt.Fprintf(out, "%s · %s · %s · ligado=%t · buscando=%t\n", a.Path, core.Clean(a.Name), a.Address, a.Powered, a.Discovering)
		}
		return printDevices(out, core.Filter(bluez.ForAdapter(s, adapter), o.search, o.filter))
	case "devices":
		ds := core.Filter(bluez.ForAdapter(s, adapter), o.search, o.filter)
		if o.json {
			return emit(ds)
		}
		return printDevices(out, ds)
	case "scan":
		a, e := bluez.SelectAdapter(s, o.adapter)
		if e != nil {
			return e
		}
		if err = c.StartScan(ctx, a); err != nil {
			return err
		}
		timer := time.NewTimer(o.duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		if err = c.StopScan(ctx, a); err != nil {
			return err
		}
		s, err = c.Snapshot(ctx)
		if err != nil {
			return err
		}
		if err = store.Update(s.Devices, nil, nil, false); err != nil {
			return err
		}
		store.Decorate(s.Devices)
		ds := core.Filter(bluez.ForAdapter(s, a.Path), o.search, o.filter)
		if o.json {
			return emit(ds)
		}
		return printDevices(out, ds)
	case "power":
		a, e := bluez.SelectAdapter(s, o.adapter)
		if e != nil {
			return e
		}
		if err = c.Power(ctx, a, o.args[1] == "on"); err != nil {
			return err
		}
		if o.json {
			return emit(map[string]any{"adapter": a.Path, "powered": o.args[1] == "on"})
		}
		_, err = fmt.Fprintf(out, "%s: Bluetooth %s\n", a.Path, o.args[1])
		return err
	}
	d, err := core.Resolve(bluez.ForAdapter(s, adapter), o.args[1])
	if err != nil {
		return err
	}
	if command == "alias" {
		if err = store.Update(nil, &d, &o.args[2], false); err != nil {
			return err
		}
		ds := []core.Device{d}
		store.Decorate(ds)
		d = ds[0]
	} else {
		if command == "connect" || command == "pair" {
			_, err = c.RegisterAgent(ctx, core.NewRequests(90*time.Second), false, false)
			if err != nil {
				return err
			}
		}
		if err = c.Action(ctx, d, command); err != nil {
			if command == "pair" || command == "connect" {
				return fmt.Errorf("%w; se houver pedido de PIN ou autorização, abra bt e use p/c e Tab para responder", err)
			}
			return err
		}
		if command == "forget" {
			if err = store.Update(nil, &d, nil, true); err != nil {
				return fmt.Errorf("removido do BlueZ, mas falha ao limpar cache: %w", err)
			}
		}
	}
	// Actions report completion, not a guessed final connection state.
	if o.json {
		return emit(map[string]any{"action": command, "address": d.Address, "adapter": d.Adapter, "label": d.Label(), "ok": true})
	}
	_, err = fmt.Fprintf(out, "%s: %s (%s, %s)\n", command, d.Label(), d.Address, d.Adapter)
	return err
}

func printDevices(out io.Writer, ds []core.Device) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NOME\tTIPO\tCONECTADO\tPAREADO\tBATERIA\tMAC\tADAPTADOR")
	for _, d := range ds {
		battery := "—"
		if d.Battery != nil {
			battery = fmt.Sprintf("%d%%", *d.Battery)
		}
		yes := func(v bool) string {
			if v {
				return "sim"
			}
			return "não"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.Label(), core.Clean(d.Type), yes(d.Connected), yes(d.Paired), battery, d.Address, d.Adapter)
	}
	return w.Flush()
}
