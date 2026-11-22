# bt

CLI Bluetooth para Linux, escrita em Go e conectada diretamente ao BlueZ por
D-Bus. `bt` abre uma interface de teclado; os subcomandos servem para uso direto
e scripts. Não executa `bluetoothctl` nem depende de sua saída.

## Instalação

Requer Linux, BlueZ 5, D-Bus do sistema e Go 1.23 ou superior para compilar.
Instale os pacotes `bluez` e `dbus` da sua distribuição e mantenha o serviço
`bluetooth` ativo. O uso normal deve ocorrer na sua sessão de usuário, sem sudo;
as permissões são determinadas pelas políticas D-Bus/Polkit da distribuição.

```sh
make build
./bin/bt
make install                  # instala em ~/.local/bin/bt
# outro destino: make install PREFIX=/usr/local
```

Ou, sem Make:

```sh
go build -buildvcs=false -trimpath -o bin/bt ./cmd/bt
install -Dm755 bin/bt "$HOME/.local/bin/bt"
```

Inclua `~/.local/bin` no `PATH`. Nenhuma biblioteca C de Bluetooth é necessária.

## Interface

Use um terminal de pelo menos 80 colunas e 22 linhas; 100×30 é mais confortável.
A busca por rádio começa somente quando você a solicita.

| Tecla | Ação |
|---|---|
| ↑/↓ ou j/k | Navegar na lista com foco |
| / | Editar busca; Enter aplica, Esc sai da edição |
| f | Alternar todos, conectados, pareados e não pareados |
| Esc fora da edição | Limpar busca e filtro |
| a | Alternar adaptador |
| o | Ligar/desligar adaptador selecionado |
| s | Iniciar/parar sessão de descoberta desse adaptador |
| c / d | Conectar / desconectar dispositivo selecionado |
| p / x | Parear / cancelar pareamento |
| n na lista | Definir apelido local; vazio remove o apelido |
| F | Esquecer; exige digitar `esquecer` e Enter |
| Tab | Alternar foco entre dispositivos e solicitações |
| y / n nas solicitações | Aceitar / rejeitar a solicitação selecionada |
| q ou Ctrl+C | Sair |

PIN e chave numérica são editados ao aceitar o pedido e enviados com Enter.
Esc abandona a edição e deixa o pedido pendente. Compare a chave de confirmação
nos dois dispositivos antes de aceitar. A entrada de PIN/chave fica mascarada.

A lista conserva a ordem das identidades existentes e acrescenta novos
dispositivos ao final. Alterações de nome, bateria e conexão não mudam a seleção.
Se o dispositivo selecionado desaparecer, nenhuma ação passa silenciosamente a
outro: use as setas para selecionar novamente. Busca, filtros e troca de
adaptador são ações explícitas que podem selecionar a primeira linha.

## Solicitações e eventos

O agente implementa `org.bluez.Agent1` com capacidade `KeyboardDisplay`. Pedidos
de confirmação, PIN, autorização de pareamento e autorização de serviço entram
em uma fila. A chamada D-Bus aguarda a resposta em uma goroutine; a interface e
outras operações continuam disponíveis. Há limite de 16 pedidos pendentes e
histórico de até 64 itens, mantido somente em memória.

Cada pedido expira localmente após 90 segundos. O BlueZ pode cancelar antes.
Aceitação, rejeição, expiração e cancelamento aparecem no painel. `DisplayPinCode`
e `DisplayPasskey` são instruções informativas para o dispositivo remoto, não
autorizações; não podem ser aceitas como se fossem pedidos. Mudanças de conexão
aparecem como eventos informativos separados. Nunca há resposta afirmativa
automática, e o programa não marca dispositivos como `Trusted`.

Enquanto a interface está aberta, o programa tenta ser o agente padrão para
receber também pedidos iniciados remotamente. Se a política impedir isso, mostra
o erro; o agente da aplicação ainda pode atender operações iniciadas pelo `bt`.
Os pedidos recebidos são conferidos contra o nome único do BlueZ no barramento.
Não é um daemon: as notificações existem enquanto a interface está executando.

Ao sair, cancela solicitações e pareamentos iniciados ainda ativos, encerra
somente suas sessões de descoberta, remove o agente e restaura o terminal.
Descobertas abertas por outros programas podem continuar. Como em qualquer
programa de terminal, SIGKILL e perda abrupta do terminal não permitem limpeza.

## Comandos e JSON

```sh
bt scan --duration 10s
bt devices
bt devices --filter paired --search fone
bt alias 'soundcore Q20i' 'Meu Fone'
bt connect 'Meu Fone'
bt disconnect 'Meu Fone'
bt pair 'Meu Fone'
bt cancel 'Meu Fone'
bt forget 'Meu Fone'          # remoção imediata no subcomando explícito
bt alias 'Meu Fone' ''        # remove somente o apelido
bt power on --adapter hci0
bt status
bt --adapter hci1             # abre interface nesse adaptador
bt devices --json
bt status --json
bt connect 'Meu Fone' --json --timeout 30s
bt scan --adapter hci1 --duration 15s --json
```

As opções podem aparecer antes ou depois do comando. Nomes, apelidos, nomes
conhecidos e MACs exigem igualdade completa, ignorando maiúsculas/minúsculas.
Busca parcial é apenas um filtro de listagem. Se houver mais de um resultado,
o erro lista os candidatos e não realiza a operação. Use MAC e `--adapter` para
desambiguar. `--` encerra opções, permitindo nomes que começam com hífen.

Sem `--adapter`, `devices`, `status` e a resolução de nomes consultam todos os
adaptadores. A interface, `scan` e `power` começam pelo primeiro caminho de
adaptador em ordem lexicográfica; `a` ou `--adapter` permite escolher outro.
Aceita `hci0`, MAC do adaptador ou caminho D-Bus completo.

`scan` é finito e devolve a lista conhecida ao final, incluindo dispositivos
pareados que não foram vistos nessa busca. O cache local de nomes não cria
dispositivos ausentes do BlueZ. Bateria só aparece se `org.bluez.Battery1`
estiver disponível. Tipo vem de Icon, Class ou Appearance; na falta deles,
aparece `desconhecido`. Não são inventados fabricante, modelo ou percentuais.

Nos subcomandos, o agente próprio rejeita pedidos que exigem entrada humana,
com orientação para abrir `bt`, usar `p`/`c` e responder via Tab. Isso permite
conectar dispositivos já pareados sem abrir uma tela e mantém scripts sem
prompts inesperados. Não há opção de aprovação automática.

Com `--json`, stdout contém somente um documento JSON. Erros são um objeto
`{"error":"mensagem"}` no stderr e saída com código 1; sucesso usa código 0.
`devices` e `scan` retornam arrays (inclusive `[]`). `status` retorna
`adapters` e `devices`; nenhum adaptador é um estado válido de status.
O campo `display_name` contém o nome preferido, e `address` mantém o MAC.
`battery` é omitido quando desconhecido. Ações retornam `ok`, `action`,
`address`, `adapter` e `label`; isso informa conclusão da chamada BlueZ,
não uma conexão futura garantida. Consulte `status` para estado atual.

## Nomes persistidos

A prioridade de exibição é: apelido local, Name do BlueZ, Alias legível do BlueZ,
último nome conhecido. Se nada legível existir, mostra
`Dispositivo sem nome · AA:BB:CC:DD:EE:FF`. O MAC completo distingue os
desconhecidos, e o adaptador aparece no detalhe.

Os nomes ficam em `$XDG_CONFIG_HOME/bt/devices.json` ou
`~/.config/bt/devices.json`. A chave combina MAC do adaptador e MAC do dispositivo
(caminho do adaptador é fallback quando seu endereço não está disponível).
Gravações usam arquivo temporário privado, rename atômico e lock entre processos.
Um cache inválido produz um erro com o caminho, sem sobrescrevê-lo silenciosamente.
Endereços BLE privados podem mudar antes do pareamento; o cache não tenta
deduzir que dois endereços aleatórios são o mesmo dispositivo.

## Problemas comuns

- Bluetooth desligado: `bt power on`; confira também `rfkill list`.
- Sem adaptador: confira conexão USB, drivers, firmware e `bt status`.
- BlueZ indisponível: confira `systemctl status bluetooth` e o D-Bus do sistema.
- Permissão negada: confira políticas D-Bus/Polkit e sua sessão de usuário.
- Falha de autenticação: confira o código e o modo de pareamento remoto;
  se necessário, esqueça e pareie novamente pela interface.
- Operação em andamento: aguarde; `x` ou `bt cancel` cancela pareamento.
- BlueZ reiniciou, agente foi liberado ou D-Bus caiu: a interface informa a
  situação e cancela pedidos; saia e abra `bt` para registrar o agente novamente.

Atualizações usam sinais `InterfacesAdded`, `InterfacesRemoved` e
`PropertiesChanged` para reconciliar uma visão completa dos objetos. Uma leitura
periódica a cada segundo também recupera mudanças em caso de sinais perdidos.

## Desenvolvimento e validação

```sh
make check                   # go vet e testes com detector de corridas
make terminal-test           # Go + dbus-daemon + pseudoterminal Linux
```

Os testes cobrem nomes e ambiguidade, ordem estável, seleção, filtros,
persistência/mesclagem/remoção, respostas explícitas e concorrentes, validação de
PIN/chave, cancelamento, expiração, decodificação e seleção de adaptadores.
Os testes de protocolo iniciam um barramento privado com um serviço de teste;
verificam chamadas D-Bus reais, sinais, agente, rejeição de remetente falso e
continuidade de leitura durante uma solicitação pendente. São fixtures de teste,
não uma implementação Bluetooth simulada oferecida ao usuário. Se `dbus-daemon`
não estiver instalado, o teste de protocolo é marcado como ignorado.

O teste de terminal verifica modo raw, tela alternativa e cursor restaurados em
saídas por `q`, Ctrl+C e SIGTERM, usando um D-Bus isolado sem BlueZ.

Nesta implementação foram validados build, vet, testes com detector de corridas,
protocolo em D-Bus privado, restauração do terminal com teste em Go e leitura do
BlueZ real com adaptador, dispositivos, bateria e busca de dois segundos.
Pareamento/autorização, conexão/desconexão, remoção de vínculo, hotplug
e múltiplos adaptadores físicos ainda precisam de validação manual com hardware.

Roteiro manual: iniciar busca; selecionar um dispositivo enquanto outros surgem;
parear com confirmação/PIN, navegar com o pedido pendente, aceitar/rejeitar,
deixar expirar e cancelar pelo dispositivo remoto; conferir bateria; desconectar
e reconectar; reiniciar a aplicação para conferir apelidos; testar adaptador
desligado, removido e dois adaptadores. Use dispositivos de teste ao esquecer
vínculos ou desligar adaptadores.

Organização: `cmd/bt` contém comandos; `internal/bluez`, transporte e agente;
`internal/core`, nomes, cache e solicitações; `internal/tui`, a interface.

Referências: [Agent1](https://bluez.readthedocs.io/en/latest/agent-api/),
[Device1](https://bluez.readthedocs.io/en/latest/device-api/) e
[Adapter1](https://bluez.readthedocs.io/en/latest/adapter-api/).
