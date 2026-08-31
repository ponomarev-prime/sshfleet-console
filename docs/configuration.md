# Справочник конфигурации

SSH Fleet Console использует TOML. YAML не является каноническим форматом.
Полный комментированный шаблон: [sshfleet.example.toml](../sshfleet.example.toml).

## Пути по умолчанию

| Данные | Путь |
|---|---|
| Application config (Linux/Unix) | `$XDG_CONFIG_HOME/sshfleet/config.toml` |
| Application config (macOS) | `$HOME/Library/Application Support/sshfleet/config.toml` |
| Application config (Windows) | `%AppData%\sshfleet\config.toml` |
| Persistent source fragments | `$XDG_CONFIG_HOME/sshfleet/sources.d/*.toml` |
| Private group fragments | `$XDG_CONFIG_HOME/sshfleet/groups.d/*.toml` |
| Private host overlays | `$XDG_CONFIG_HOME/sshfleet/hosts.d/*.toml` |
| Remote source state/cache | application default или `[app].source_state_dir` |
| Installed application data | `$XDG_DATA_HOME/sshfleet` |
| Launcher | `~/.local/bin/sshfleet` |
| Optional short alias | `~/.local/bin/sf`, only when the name is free |
| Transitional alias | `~/.local/bin/sshf`, supported until `v1.0.0` |

При незаданных XDG variables обычно используется `~/.config` и `~/.local/share`.
Explicit `--config` обязан существовать; отсутствующий default config означает
валидные встроенные defaults.

На macOS и Windows `sources.d`, `groups.d` и `hosts.d` находятся рядом с
application config в platform user-config directory. Точный group path можно
переопределить через `[app].groups_dir` или `--groups-dir`; относительные и
`~`-paths нормализуются до чтения.

## Приоритет настроек

От меньшего к большему приоритету:

1. compiled safe defaults;
2. главный application TOML;
3. persistent `sources.d`, private `groups.d` и host overlays;
4. repeatable launch sources (`--ssh-config`, `--inventory`, `--local-config`);
5. one-shot CLI overrides (`--no-probe`, `--max-concurrent` и другие).

`~/.ssh/config` — отдельный встроенный source `user`: загружается по умолчанию,
если его не отключили TOML или `--no-user-ssh-config`. `--user-ssh-config`
временно включает его обратно. Противоречивые пары flags отклоняются.

## CLI

| Flag/command | Назначение |
|---|---|
| `--config PATH` | explicit main TOML |
| `--ssh-config [NAME=]PATH` | trusted OpenSSH source на один run; repeatable |
| `--inventory [NAME=]PATH` | restricted inventory на один run; repeatable |
| `--local-config [NAME=]PATH` | trusted localhost/local-sshd targets на один run; repeatable |
| `--sources-dir PATH` | persistent source fragments directory |
| `--groups-dir PATH` | private group fragments directory |
| `--overrides-dir PATH` | private host overlay directory |
| `--editor EXECUTABLE` | editor без shell arguments |
| `--shell EXECUTABLE|auto` | local default shell на один run |
| `--shell-arg VALUE` | отдельный local shell argv на один run; repeatable |
| `--no-user-ssh-config` | не читать `~/.ssh/config` |
| `--user-ssh-config` | включить user source поверх TOML |
| `--no-default-ssh-config` | deprecated alias отключения user source |
| `--no-probe` / `--probe` | временно выключить/включить probes |
| `--max-concurrent N` | simultaneous probe ceiling |
| `--refresh-interval DURATION` | probe interval, например `5s` |
| `--list` | вывести resolved inventory и завершиться |
| `--version` | короткая build version |
| `version [--json]` | полная provenance |
| `healthcheck [--strict]` | capabilities; aliases `doctor`, `checkhealth` |
| `source add ...` | persistent source wizard/non-interactive add |
| `source pack ...` | signed + age encrypted restricted bundle |
| `credential set NAME` | скрытый ввод и запись в Secret Service |

### `source add`

Интерактивный вызов спрашивает недостающие обязательные значения. Полностью
non-interactive форма использует:

| Option | Значение |
|---|---|
| `--type` | `openssh`, `local`, `inventory`, `encrypted` или `remote` |
| `--name` | уникальное source name |
| `--path` | config/inventory file или encrypted bundle directory |
| `--url` | HTTPS bundle directory URL для remote |
| `--signing-key` | pinned OpenSSH `allowed_signers` file |
| `--age-identity-ref` | `secret-service:KEY` или `age-plugin:PATH` |
| `--auth-credential` | declared bearer credential для HTTPS |
| `--sources-dir` | destination fragments directory |
| `--config` | main TOML для defaults/credential declarations |

### `source pack`

```sh
sshfleet source pack --source-id shared --revision 1 \
  --inventory ~/inventory/shared.toml \
  --output ~/inventory/shared.bundle \
  --recipient age1... \
  --signing-key ~/.ssh/inventory_signing \
  --expires 720h
```

`--revision` обязан быть положительным и монотонным; `--expires` по умолчанию
30 дней. Output directory новый, plaintext туда не записывается. Обязательные
string values можно безопасно ввести в wizard prompts, но signing key/recipient
не являются password storage.

### Credentials, health и version

- `sshfleet credential set NAME [--config PATH]` принимает value дважды с disabled
  terminal echo, очищает buffers после использования и требует declared entry.
- `sshfleet healthcheck [--config PATH] [--shell EXECUTABLE] [--shell-arg VALUE] [--strict]`; aliases: `doctor`,
  `checkhealth`. Strict возвращает non-zero и для missing optional capabilities.
- `sshfleet version [--json]` не обращается к Git: provenance уже в binary.

## Глобальный `[terminal]`

```toml
[terminal]
default_shell = "auto"
shell_args = []
```

Это оболочка только локальной рабочей станции. Она не меняет shell удалённого
SSH host и не участвует в `app.containers.shell_priority`. Trusted direct
`local_config` без собственного `shell` наследует effective global shell;
явный host shell остаётся host-level override.

Приоритет выбора: `--shell`/repeatable `--shell-arg` для одного запуска →
application TOML → OS auto-detection. Linux/macOS auto сначала проверяет
`$SHELL`, затем системные candidates; Windows — `%COMSPEC%`, PowerShell и cmd.
`healthcheck` и Preview direct localhost показывают configured/effective origin.
Если явная shell отсутствует, hidden fallback запрещён: local action вернёт
понятную ошибку и список найденных вариантов, а SSH inventory продолжит работу.

`shell_args` — массив argv, не строка shell-кода. Unicode и пробелы внутри
отдельного аргумента сохраняются. Путь executable с пробелами допустим как
явный path; `default_shell = "sh -c"` отклоняется строгой схемой.

CLI smoke без пользовательского SSH config:

```sh
sshfleet --no-user-ssh-config --local-config workstation=./local.toml \
  --shell /bin/fish --shell-arg=-l
```

## Полный `[app]`

```toml
version = 1

[app]
refresh_interval = "10s"
connect_timeout = "6s"
# max_concurrent = 16
ssh_binary = "ssh"
probe_enabled = true
load_user_ssh_config = true
# sources_dir = "~/.config/sshfleet/sources.d"
# groups_dir = "~/.config/sshfleet/groups.d"
# overrides_dir = "~/.config/sshfleet/hosts.d"
editor_priority = ["nvim", "vim", "nano"]
# editor = "nvim" # compatibility shortcut, priority list preferred
# workspace_bundle = "~/.local/share/sshfleet/sshfleet-tools-linux-amd64.tar.gz"
workspace_cleanup = true
source_fetch_timeout = "10s"
source_max_bytes = 4194304
# source_state_dir = "~/.cache/sshfleet/sources"

[app.containers]
enabled = true
runtimes = ["docker", "podman"]
refresh_interval = "2s"
include_stopped = false
shell_policy = "first_available"
shell_priority = ["/bin/bash", "/bin/ash", "/bin/sh"]

[app.ui]
sources_width_percent = 10
preview_width_percent = 24
host_column_percent = 30
```

### Поведение полей

- `refresh_interval` и `connect_timeout` должны быть положительными Go durations.
- `max_concurrent` при отсутствии/нуле равен `2 × runtime.GOMAXPROCS`, минимум 1.
- `ssh_binary` разрешается сначала среди SSH Fleet-owned tools, затем через PATH.
- `editor_priority` выбирает первый доступный executable в том же порядке;
  default `nvim`, `vim`, `nano`.
- `workspace_cleanup = true` удаляет temporary remote workspace после любого
  завершения; false предназначен для диагностики.
- `source_fetch_timeout` и `source_max_bytes` ограничивают remote fetch.
- неизвестные поля запрещены.
- `app.containers.enabled` включает динамические локальные Docker/Podman targets;
  runtime вызывается без `sudo`, а ошибка одного daemon изолирована в его source.
- `runtimes` допускает только `docker` и `podman`; `shell_policy` сейчас строго
  равен `first_available`, а `shell_priority` содержит только абсолютные
  executable paths. Shell проверяется только при открытии терминала;
  read-only inspect работает для stopped, distroless и scratch containers.
  `refresh_interval` обновляет список независимо от SSH probe interval.
- `include_stopped = true` включает stopped containers. Ошибка runtime не удаляет
  последний успешный snapshot немедленно: он помечается `stale`, а причина
  остаётся видимой. Runtime sources различают `loaded`, `empty`, `partial`,
  `denied`, `unavailable` и `stale`.

### UI widths

- `sources_width_percent`: 8–30;
- `preview_width_percent`: 12–35;
- сумма side panes: не более 48%;
- `HOSTS = 100 - SOURCES - PREVIEW` и всегда остаётся главным;
- `host_column_percent`: 15–60, доля name column внутри центральной таблицы.

Меньший `host_column_percent` отдаёт больше места CPU%/MEM% bars, но не меняет
внешние панели. `?` показывает configured shares и фактические cell widths.

## Source declarations

### Trusted local OpenSSH

```toml
[[sources]]
name = "lab"
kind = "ssh_config"
path = "~/.ssh/lab.conf"
```

Это trusted local code, а не data-only inventory. Literal `Host` aliases и
`Include` enumerated; patterns применяются OpenSSH при `ssh -G`, но не создают
конкретные строки сами.

### Trusted local targets

```toml
[[sources]]
name = "workstation"
kind = "local_config"
path = "~/.config/sshfleet/local.toml"
```

Сам файл имеет отдельную строгую схему:

```toml
version = 1

[[local_hosts]]
alias = "this-machine"
name = "Local workstation"
mode = "direct"
shell = "/bin/fish"
shell_args = ["-l"]
working_directory = "~/my_code"
tags = ["local"]

[[local_hosts]]
alias = "local-sshd"
mode = "ssh"
host = "127.0.0.1"
port = 2222
user = "demo"
shell = "/bin/bash"
```

`shell`/`shell_args` в direct entry можно опустить: тогда применяется global
`[terminal]`. `direct` выполняет выбранный executable локально с отдельным argv и запрещает
SSH routing/credentials. `ssh` использует OpenSSH, но routing берётся только из
этой записи. Такой source считается trusted local code и поэтому не разрешён в
encrypted/remote inventory. Добавление: repeatable `--local-config NAME=PATH`
или `sshfleet source add --type local ...`.

### Restricted local inventory

```toml
[[sources]]
name = "stands"
kind = "inventory"
path = "~/inventory/stands.toml"
```

Source file имеет закрытую схему `version`, `[[hosts]]`, `[[groups]]` с routing,
tags, probe policy и logical credential/identity references. App settings,
source chaining, passwords, commands и executable OpenSSH directives запрещены.

### Encrypted local inventory

```toml
[[sources]]
name = "shared"
kind = "encrypted_inventory"
path = "~/inventory/shared.bundle"
signing_key = "~/.config/sshfleet/allowed_signers"
age_identity_ref = "secret-service:sshfleet/age/shared"
```

Bundle содержит `manifest.toml`, `manifest.sig`, `inventory.toml.age`.

### Remote inventory

```toml
[[sources]]
name = "central"
kind = "remote"
url = "https://inventory.example/fleet/"
auth_credential = "inventory-api"
signing_key = "~/.config/sshfleet/allowed_signers"
age_identity_ref = "secret-service:sshfleet/age/central"
```

Разрешён только restricted encrypted bundle по HTTPS. Remote `ssh_config`
запрещён. Redirect, oversize, invalid signature/hash, expiry и rollback fail
closed.

## Hosts и overlays

Главный config может enrich существующий alias или объявить direct host:

```toml
[[hosts]]
alias = "api-01"
name = "Production API"
source = "user"
hostname = "192.0.2.10"
user = "operator"
port = 22
proxy_jump = "bastion"
tags = ["prod", "api"]
probe = true
credential = "prod-password"
identity = "prod-key"
```

Overlay поддерживает те же presentation/routing поля, но хранится отдельно и
не изменяет base source. Пара `source + alias` — identity записи.

## Credentials и identities

TOML содержит только lookup reference:

```toml
[[credentials]]
name = "prod-password"
type = "password" # password | bearer | key-passphrase
provider = "secret-service"
key = "sshfleet/prod/password"

[[credentials]]
name = "prod-key-passphrase"
type = "key-passphrase"
provider = "secret-service"
key = "sshfleet/keys/prod"

[[identities]]
name = "prod-key"
path = "~/.ssh/id_ed25519_prod"
credential = "prod-key-passphrase"
```

`path` разрешён только в private local main config и должен указывать на regular
mode-0600 private key. Shareable inventory содержит `identity = "prod-key"`, но
не workstation path или key bytes.

Значения задаются только интерактивно:

```sh
sshfleet credential set prod-password
```

## Host rules

Привязка credential к набору alias:

```toml
[[host_rules]]
source = "stands"
match = "database-*"
credential = "production-databases"
```

`source` можно опустить. Конкретная host declaration имеет более точный смысл,
чем широкое rule; ambiguous/invalid references отклоняются validation.

## Cross-source groups

```toml
[[groups]]
name = "production-api"
members = ["user:api-01", "lab:api-02"]
match = ["stands:api-*"]
```

Стабильный selector — `source:alias`; plain alias допустим, но нежелателен при
duplicates. `match` использует filepath-style patterns по обоим forms. Groups
не переписывают underlying source.

Группы из main TOML редактируются через `c`. Созданные из TUI группы лежат по
одной в mode-0600 fragments под `[app].groups_dir` или `--groups-dir`; поддержаны
Unicode-имена, empty groups, rename/delete и membership по `source:alias`.
Unknown fields, duplicate names, symlinks и unsafe control characters
отклоняются. Overlay не может молча затенить одноимённую группу main config.

Пример fragment, создаваемого приложением:

```toml
version = 1

[[groups]]
name = "stands-202-203"
members = ["user:202", "user:203"]
match = ["secure-perf:xv15-*"]
```

`version = 1` обязателен; один файл содержит ровно один `[[groups]]`. Имя файла
вычисляется приложением и является opaque implementation detail — связь задаёт
поле `name`. `members` содержит exact stable IDs `source:alias`, а необязательный
`match` — filepath-style glob по stable ID или alias. Fragment не может
содержать secrets, identity bytes, credentials или command presets.

## Command presets

```toml
[[command_presets]]
name = "uptime"
argv = ["uptime"]
timeout = "15s"
max_concurrent = 4
```

Это locally trusted app config. `argv` — массив аргументов, не shell snippet;
empty/control-bearing values запрещены. Default timeout 30s. Ноль в
`max_concurrent` наследует application ceiling. Restricted/remote inventory
не может объявить команды.

## Persistent source fragments

`sshfleet source add` создаёт отдельный mode-0600 TOML под `sources.d` через
temporary file + atomic rename. Это удобнее ручного изменения main config и
сохраняет origin каждого source. CLI sources остаются ephemeral.

## Environment

Публичные `SSHF_*` variables используются launcher/AskPass/workspace internals и
не заменяют пользовательский configuration API. Не помещайте secrets в
environment вручную. Для portable paths используйте XDG variables, flags или
TOML fields.
