# Руководство пользователя

Это полный справочник ежедневной работы с SSH Fleet Console. Для установки и
пятиминутного старта начните с [README](../README.md).

Каноническая команда — `sshfleet`. Установщик также сохраняет переходный
`sshf` до `v1.0.0` и создаёт короткий `sf`, только если это имя ещё не занято
другой программой. Документация и новые scripts всегда используют `sshfleet`.

## Дисклеймер и границы

SSH Fleet Console — локальная клавиатурная оболочка над установленным OpenSSH,
а не собственный SSH protocol stack, SSH-server или постоянный remote agent.
Программа читает только явно разрешённые sources, запускает ограниченные
read-only probes и открывает привычный OpenSSH terminal. Она не отключает
host-key verification, не принимает изменившийся ключ автоматически и не
хранит secret values в TOML, argv, logs или session tail.

Trusted OpenSSH config является локальным кодом: `ProxyCommand`,
`KnownHostsCommand`, `Match exec` и похожие directives могут выполнять
программы на рабочей станции. Подключайте такой source только если доверяете
файлу. Для переносимых и remote sources используйте restricted inventory — он
не принимает executable directives и всегда изолируется через `ssh -F /dev/null`.

## Быстрый ежедневный старт

```sh
sshfleet -v                    # какая сборка запущена
sshfleet healthcheck          # OpenSSH, shell, editor, Secret Service, tools
sshfleet                      # ~/.ssh/config загружается как source user
```

Если нужно сначала только увидеть resolved inventory и не подключаться:

```sh
sshfleet --no-probe --list
```

Изолированный запуск с одним тестовым OpenSSH config:

```sh
sshfleet --no-user-ssh-config --ssh-config lab=~/.ssh/lab.conf
```

В TUI нажмите `/` и введите часть alias, source, tag, target или имени группы.
Выберите host, нажмите `Enter` и оставьте первый пункт `Open terminal tab
(default)`. `Ctrl+D` закрывает активную terminal tab и возвращает Fleet.

## Экран

Интерфейс адаптируется к ширине терминала:

- `SOURCES` — узкая навигация. `All available` всегда первый, затем локальные,
  encrypted/remote sources. Ниже отдельная секция `GROUPS` с private groups и
  количеством узлов; происхождение host при этом не меняется. Последняя секция
  `VIEWS` содержит read-only выборки по текущему состоянию fleet.
- `HOSTS` — главный и самый широкий столбец. Первая строка содержит alias/name,
  CPU cores, total RAM, total swap, CPU% и MEM%. Шкалы повторяют принцип dtop:
  `█` — использованная часть, `░` — оставшаяся, процент показан отдельно;
  нейтральное серое выделение строки не скрывает шкалу.
  Вторая — top CPU process, PID/state/CPU и возраст probe.
- `PREVIEW` — объяснение выбранной строки. Здесь отдельно отмечены данные
  рабочей станции (`LOCAL`) и выбранного сервера (`REMOTE`).

При недостаточной ширине панели последовательно складываются в один экран.
Во время embedded terminal Preview временно расширяется и после выхода точно
возвращает размеры из `[app.ui]`.

## Клавиши

| Контекст | Клавиши | Результат |
|---|---|---|
| Любой список | `j/k`, `↑/↓` | следующая/предыдущая строка |
| Любой список | `g/G`, `Home/End` | первая/последняя строка |
| Панели | `Tab`, `h/l`, `←/→` | Sources ↔ Hosts |
| Sources/Groups/Views | `Enter` | применить выбранную строку и перейти в Hosts |
| Sources | `n` | создать пустую private group в `groups.d` |
| Host | `m` | открыть membership и добавить/убрать stable `source:alias` |
| Group | `R`, `D`, `e` | переименовать, удалить или открыть fragment в editor |
| Hosts | `/` | глобальный живой поиск host по всему fleet |
| Search | `Esc` | закрыть ввод; повторный `Esc` очищает поиск и возвращает прежний Source/Host |
| Host | `Enter` | контекстное Actions menu |
| Host | `e` | private TOML overlay выбранного host |
| Любой экран | `c` | главный application TOML; restart применяет изменения |
| Group | `x` | command preset → immutable plan → confirm |
| Host | `Shift+K` | inspect/confirm changed-host-key repair |
| Любой экран | `r` | новый bounded probe sweep |
| Любой экран | `?` | application healthcheck |
| Menu/dialog | `Esc` | закрыть без действия |
| Приложение | `q`, `Ctrl+C` | выйти |

В редакторе `nvim` стрелки читает сам редактор: TUI на время полностью
приостанавливается и восстанавливается после его выхода. Если вместо редактора
печатаются escape sequences, откройте `?`: там указан фактически найденный
editor и origin. Приоритет по умолчанию — owned `nvim`, system `nvim`, `vim`,
`nano`.

## Глобальный поиск host

Нажмите `/` из любого Source или Group и сразу печатайте запрос. Пока строка
ввода открыта, результаты обновляются на каждой клавише по всему fleet, а не
только внутри текущего левого фильтра. На время поиска выделяется `All available`;
под найденным host показывается `source: NAME`, а Preview содержит его полный
origin.

Ищутся безопасные не-секретные поля: alias и display name, stable ID, source,
target hostname, user, port, ProxyJump, tags, groups, transport и container
runtime/ID/image/status. Для OpenSSH alias также используется уже разрешённый
локально effective target. Запрос регистронезависимый; слова разделяются
пробелами и должны совпасть все:

```text
/202
/perf 202
/deploy 192.0.2.77
/docker postgres
```

`Enter` фиксирует найденный список и оставляет обычную навигацию/Actions menu.
Первый `Esc` во время ввода только закрывает строку, второй очищает запрос и
восстанавливает Source/Group и host, выбранные до поиска. Пустой запрос сразу
возвращает исходный контекст. Поиск не меняет inventories, groups или overlays.

## Вычисляемые Views

Секция `VIEWS` не является ещё одним источником и ничего не сохраняет на диск.
Она каждый раз вычисляется из последнего безопасно полученного probe/source
state:

- `Offline` — target недоступен по сети;
- `Errors` — authentication, changed host key или ошибка probe;
- `CPU ≥ 80%` — валидная CPU delta показывает загрузку не ниже 80%;
- `MEM ≤ 20%` — доступно не больше 20% общей RAM;
- `Stale` — динамический target оставлен из последнего успешного snapshot после
  ошибки нового discovery.

Выберите View слева и нажмите `Enter`. Список `HOSTS` останется обычным: доступны
Preview, Actions menu, terminal tabs и refresh. `/` временно ищет по всему fleet;
после очистки поиска приложение возвращает выбранный View по имени, даже если
динамический source добавился или исчез. Пустой View показывает объяснение, а не
выглядит как поломанный source.

## Меню Actions

### Обычный Linux host

Пункты появляются только при наличии capability:

1. `Open terminal tab (default)` — запускает OpenSSH в отдельном реальном PTY,
   оставляя постоянную вкладку Fleet и остальные сессии живыми. Верхняя строка
   показывает `●` running, `✓` exited и `!` error.
2. `Open terminal in Preview` — настоящий resize-aware PTY и VT emulator внутри
   Preview; `Ctrl+]` немедленно возвращает в fleet.
3. `Open SSH Fleet workspace` — отдельная вкладка с временным shell, в котором уже доступны
   проверенные `lf`, `nvim`, `dtop` и `bat` с нашими конфигами. После выхода
   workspace удаляется согласно `workspace_cleanup`.
4. `Refresh host` — probe только выбранной строки.
5. `Edit host overlay` — private application layer, base source не меняется.
6. `Edit source SSH config` — только для доверенного локального OpenSSH source.

Первые два пункта не подмешивают bundle и сохраняют исходный remote `PATH`.
Поэтому `command -v lf nvim dtop bat` там показывает только программы, уже
установленные на сервере. Для наших временных инструментов нужен именно
`Open SSH Fleet workspace`.

После SSH/ Git session Preview хранит до 12 последних printable lines. Буфер
ограничен 128 KiB, очищает ANSI/control data, живёт только в памяти и не
записывается на диск. После завершения активной terminal tab интерфейс
автоматически возвращается во Fleet; вкладка с `✓` сохраняет финальный VT screen
до закрытия и доступна через `Ctrl+G`, затем обычный номер. Preview terminal показывает
собственный VT screen.

Активная terminal tab также имеет полноценный локальный VT-scrollback. Колесо
мыши листает его по три строки и показывает в footer `SCROLL N/M`; событие не
уходит в remote shell как стрелка вверх/вниз. Колесо вниз возвращает live-низ.
Если начать печатать, вкладка сначала возвращается к live-экрану, а затем
передаёт эту клавишу PTY. Объём задаётся в главном config:

```toml
[terminal]
scrollback_lines = 10000 # 1..100000, отдельный bounded RAM-buffer на вкладку
```

Scrollback не пишется на диск и исчезает при закрытии вкладки или приложения.
Это не то же самое, что ограниченный sanitized `LAST SESSION` в Preview.

В terminal tab `Ctrl+D` — локальное быстрое закрытие: вкладка немедленно
удаляется, интерфейс возвращается во Fleet, а PTY останавливается в фоне. Для
штатного OpenSSH disconnect без удаления завершённой вкладки можно выполнить
`exit` или `~.` в начале строки. Для точного выбора нажмите `Ctrl+G`, отпустите,
затем нажмите обычную `1…9`: `1` — Fleet, `2` — первая сессия. После `Ctrl+G`
приложение сразу показывает Fleet и footer `TAB SELECT`; `Esc` отменяет режим,
а несуществующий номер оставляет его открытым с объяснением. Эта двухтактная
последовательность работает даже когда Konsole или другой внешний terminal
emulator забирает `Alt+цифра`. `Ctrl+N`/`Ctrl+P` циклически переключают вкладки.
`Alt+1…9` и `Ctrl+1…9` остаются только compatibility shortcuts и работают лишь
когда terminal emulator передаёт modified-number приложению.
Обычная `q` внутри живой terminal tab передаётся удалённой программе и не
закрывает SSH Fleet Console.
Bracketed paste передаётся
активному вложенному PTY целиком и не становится локальными hotkeys. `Ctrl+]`
сначала предупреждает о живом foreground process; повторное
нажатие закрывает PTY, `Esc` отменяет. В Preview `Ctrl+]` закрывает Preview сразу.

### Localhost и локальные containers

`local_config` direct host предлагает configured local shell во вкладке/Preview
терминале, local refresh и правку trusted source. Временное и постоянное
подключение:

```toml
[terminal]
default_shell = "auto"
shell_args = []
```

Если direct entry не содержит `shell`, она наследует этот global default.
One-shot override: `sshfleet --shell /bin/fish --shell-arg=-l`. В Preview direct
localhost виден `shell origin`; `?` и CLI healthcheck показывают OS, effective
path и PTY capability. Явная отсутствующая shell не получает скрытый fallback.
Remote SSH и container shell policy от `[terminal]` не зависят.

```sh
sshfleet --no-user-ssh-config --local-config workstation=~/.config/sshfleet/local.toml
sshfleet source add --type local --name workstation --path ~/.config/sshfleet/local.toml
```

Docker/Podman source показывает context/endpoint и состояние runtime. Для каждого
container Preview получает read-only inspect-метаданные без запуска shell:
immutable ID, image/platform, state/health, entrypoint/cmd, restart policy,
mounts, networks и ports. Поэтому stopped, distroless и scratch targets можно
диагностировать, даже если терминал открыть невозможно.

Running container предлагает shell в полном/Preview терминале, follow последних
200 log lines и runtime refresh. Policy `first_available` проверяет
`shell_priority` по порядку и показывает effective shell после выбора. Для
stopped container shell-пункты скрыты. `Ctrl+C` завершает follow-logs и возвращает
TUI без ложной ошибки и в Docker, и в Podman. Console не вызывает `sudo`, не
копирует ключи в container и не форвардит SSH agent автоматически.

### Git endpoint

Alias с effective user `git` не опрашивается Linux shell-командой. Меню запускает
`ssh -T` без remote command и показывает `git-access` при успешной key handshake.
Это проверяет доступ к сервису, но не авторизацию конкретного repository.

### Changed host key

Это security event, а не обычная ошибка:

1. Нажмите `Shift+K` для read-only inspection effective `HostKeyAlias`, port,
   `UserKnownHostsFile`, presented fingerprint и сохранённых fingerprints.
2. Сверьте fingerprint через независимый канал: console/hypervisor, CM,
   администратора или signed inventory.
3. Снова `Shift+K`: приложение создаст уникальный timestamped backup рядом с
   `known_hosts`, изменит temporary copy через `ssh-keygen -R`, проверит race и
   atomically установит результат.
4. Откройте terminal tab. Первый repair connection принудительно использует
   `StrictHostKeyChecking=ask` и `UpdateHostKeys=no`.

Автоматического доверия `ssh-keyscan`, `StrictHostKeyChecking=no` и silent
replacement нет. Symlinked/ambiguous files и concurrently changed originals
отклоняются.

## Preview: откуда взялись данные

- `CONNECTION · LOCAL` — source name/path, alias, resolved target/user/port,
  proxy, identity и только credential binding без значения секрета.
- `SSH CLIENT · LOCAL` — абсолютный путь и версия OpenSSH, который запускает
  приложение на рабочей станции.
- `HOST STATUS · REMOTE` — latency, cores, total/available RAM, total/available
  swap, uptime, root filesystem и top process.
- `SSH SOFTWARE · REMOTE` — remote OpenSSH client/server, sshd unit/socket,
  ssh-agent, scp/sftp/ssh-keygen/ssh-add и OpenSSL.
- `SYSTEM · REMOTE` — distribution, kernel/architecture, init, virtualization,
  systemd, Docker, containerd, Podman и kubelet.
- `LAST SESSION · REMOTE` — sanitized bounded tail последнего действия.

Не каждый Unix host предоставляет все поля. Недоступная capability отображается
как отсутствующая/unknown и не ломает остальные результаты.

## Редактирование

### Главный config

Нажмите `c`. Откроется фактический application TOML. После выхода перезапустите
`sshfleet`, чтобы применить app/source settings. Схема строгая: опечатка не будет
молча принята.

### Host overlay

Нажмите `e`. Файл создаётся в `~/.config/sshfleet/hosts.d` (или configured
`overrides_dir`) и идентифицирует base host неизменяемой парой `source + alias`:

```toml
version = 1

[[hosts]]
source = "user"
alias = "api-01"
name = "Production API"
tags = ["prod", "api"]
probe = true
```

Base OpenSSH config/inventory не переписывается. Реальный alias не переименяется,
иначе потеряется наследование его `Host` block. Passwords и private keys overlay
не принимает.

### Source config

Для trusted local `ssh_config` соответствующий Actions item открывает именно его
path. Restricted, encrypted и remote inventory не превращаются в executable
OpenSSH config и таким способом не редактируются.

## Sources

Один запуск с дополнительными source:

```sh
sshfleet --ssh-config work=~/.ssh/work.conf --inventory lab=~/fleet/lab.toml
```

Полностью изолированный запуск:

```sh
sshfleet --no-user-ssh-config --ssh-config lab=~/.ssh/lab.conf
```

Постоянное добавление:

```sh
sshfleet source add
sshfleet source add --type openssh --name lab --path ~/.ssh/lab.conf
sshfleet source add --type inventory --name stands --path ~/inventory/stands.toml
```

Declarations записываются atomically с mode `0600` в `sources.d`. Полный manual
с безопасными temporary fixtures: [manual-isolated-config.md](manual-isolated-config.md).

## Группы и команды

### Group membership и CRUD

Groups — private application layer поверх любых sources. Они находятся отдельной
секцией ниже SOURCES; source и исходный SSH/inventory config не переписываются.

1. В левой панели нажмите `n`, введите имя и подтвердите Enter.
2. Вернитесь в `All available` или нужный source и выберите host.
3. Нажмите `m` либо `Enter` → `Manage group membership`.
4. Выберите группу и нажмите Enter/Space. Сохраняется стабильный `source:alias`.
5. На строке группы доступны `R` rename, `D` delete и `e` editor.

Keyboard-created group хранится отдельным mode-0600 fragment в
`~/.config/sshfleet/groups.d`. Запись идёт через private temporary file, sync и
atomic rename. Группа из главного `config.toml` остаётся read-only для CRUD:
её можно изменить через `c`, поэтому TUI не переписывает чужой блок TOML.

Имя файла — непрозрачный стабильный hash имени группы, например
`group-96f1a4a699ddefd2.toml`; на UX оно не влияет. Каждый fragment содержит
ровно одну группу:

```toml
version = 1

[[groups]]
name = "XV15"
members = ["user:202", "user:203"]
match = ["secure-perf:xv15-*"]
```

`members` — точные stable IDs `source:alias`; `match` — необязательные
filepath-style glob-паттерны. Пароли, private key bytes, credentials и commands
в group fragment не записываются. `e` открывает fragment выбранной группы.

Для групповой команды выберите group, нажмите `x`, выберите локальный
`command_preset`, изучите exact host snapshot и argv, затем подтвердите второй
раз. Empty group допустима, но команда на ней fail-closed и не запускается.

OpenSSH получает quoted argv как данные, без локального shell parsing, TTY,
forwarding и local commands. Fan-out ограничен `max_concurrent`; timeout,
stdout/stderr и result разделены по host и sanitized. Remote/restricted
inventories не могут добавлять command presets.

## Portable remote workspace

Bundle используется только по явному action. До upload проверяются local archive
и SHA-256 sidecar, безопасные tar entries и remote `Linux/x86_64` + glibc 2.34+.
Upload идёт по OpenSSH stdin в private mode-0700 `/tmp/sshfleet-workspace.*`.

Внутри workspace запустите `lf`, `nvim`, `dtop` или `bat` как обычные команды.
Workspace выбирает оболочку из `$SHELL` без проверки имени дистрибутива. Для
`bash`, `zsh`, `fish` и POSIX shells временный ENV-overlay сначала загружает
обычный пользовательский rc с исходными `PATH`/`XDG_CONFIG_HOME`, затем
добавляет bundle поверх него. Файлы пользователя на цели не изменяются.
В `lf` Enter открывает каталог штатно, а файл — через `bat`: до 50 строк без
pager, свыше 50 строк через `less`, если он доступен. Клавиша `e` выбирает первый
доступный редактор в порядке `nvim → vim → nano`; bundled `nvim` найден первым.
Если `less` отсутствует, длинный файл использует встроенный интерактивный pager
`bat`, поэтому листание работает и на минимальной системе.

Distroless-контейнеры, в которых нет shell или `tar`, не поддерживаются этим
SSH-механизмом. Для них запланирован отдельный container transport: без
предположения о наличии пользовательской оболочки внутри image.

Не копируются dotfiles, credentials, keys, agent sockets, SSH config, Docker
socket или package manager state. При `workspace_cleanup = true` directory
удаляется на normal, error и signal exit. `false` нужен только для диагностики.

## Healthcheck

В TUI нажмите `?`, либо выполните:

```sh
sshfleet healthcheck
sshfleet doctor
sshfleet healthcheck --strict
```

Отчёт показывает build provenance, OS/architecture, effective local shell,
PTY/ConPTY status, обязательный OpenSSH, credential/source/editing/workspace
capabilities, absolute path и origin `cli|toml|os-auto|sshfleet|system|missing`,
impact и remediation. Обычный режим падает только без обязательной capability;
`--strict` считает WARN ошибкой и подходит для CI/full workstation.

## Диагностика

```sh
sshfleet -v
sshfleet --version
sshfleet version --json
sshfleet --no-probe --list
sshfleet healthcheck
```

`-v` — короткий совместимый alias `--version`; обе команды должны вывести
одинаковую строку. `version --json` содержит полный commit, branch, channel,
source date, clean/dirty state, Go version и platform.

Source errors локализуются: недоступный host/source/tool не должен повреждать
экран или останавливать остальные probes. Любой внешний text проходит границы
size/UTF-8/control-sequence sanitization.
