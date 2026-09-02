# Глоссарий SSH Fleet Console

## SemVer

**Semantic Versioning** — формат `MAJOR.MINOR.PATCH`:

- `MAJOR` увеличивается при несовместимом изменении публичного контракта;
- `MINOR` — при новой обратно совместимой функциональности;
- `PATCH` — при обратно совместимом исправлении или security hardening.

Пример: после `v1.4.2` совместимый bugfix становится `v1.4.3`, новая совместимая
возможность — `v1.5.0`, несовместимый контракт — `v2.0.0`.

До `v1.0.0` API считается развивающимся. Политика SSH Fleet Console: `MINOR`
может содержать заметные продуктовые изменения, `PATCH` оставлен для fixes и
security. Первый public release `v0.1.0` опубликован 2026-08-31.

Stable release tag должен быть строго `vX.Y.Z`. `dev-14061d49c195` — не SemVer
release, а provenance development build: branch/channel + 12-symbol commit SHA.
`+dirty` означает незакоммиченные source changes. `sshfleet -v` и
`sshfleet --version` печатают одинаковую компактную provenance-строку;
`sshfleet version --json` возвращает полный машинно-читаемый набор полей.

## SSH Fleet

Зонтичная экосистема для Console, будущего Web и Hub. Компоненты живут в разных
repositories, имеют независимые release cycles и общаются через versioned
schemas/API.

## SSH Fleet Console / `sshfleet`

Локальное Go TUI из repository `sshfleet-console`. `sshfleet` — каноническая
команда. `sf` — необязательный alias, который installer создаёт только при
отсутствии конфликта. `sshf` — переходный legacy-alias до `v1.0.0`; namespace
config/data/credentials остаётся `sshfleet`.

## Fleet

Набор SSH resources, объединённых sources и private groups, которые пользователь
видит как один рабочий inventory.

## Host / alias / target

- **Host** — одна строка/ресурс в Console.
- **Alias** — стабильное имя, передаваемое OpenSSH и совпадающее с literal
  `Host` либо inventory key.
- **Target** — effective `user@hostname:port`, вычисленный через config/`ssh -G`.

Display `name` можно изменить overlay, не меняя alias.

## Source

Именованный origin hosts. Реализованы built-in user OpenSSH config, additional
trusted OpenSSH config, restricted inventory, encrypted local bundle и remote
bundle. `All available` — виртуальное объединение, а `@ group` — private view,
не source данных.

## Trusted OpenSSH config

Локальный `ssh_config`, которому разрешён полный OpenSSH semantics. Это code
trust boundary, потому что directives могут запускать local programs.

## Restricted inventory

Закрытая data-only TOML schema для hosts/groups, routing metadata, tags, probe
policy и logical secret references. Не принимает passwords, commands,
ProxyCommand, source chaining и unknown fields.

## Encrypted inventory bundle

Restricted inventory в age ciphertext вместе с SSHSIG-signed manifest. Подпись
отвечает за authenticity/integrity/policy, age — за confidentiality.

## Credential reference

Non-secret tuple `name/type/provider/key` в private TOML. Само значение хранит
Secret Service/KWallet compatibility API.

## Logical identity

Shareable имя encrypted OpenSSH key, которое local main config связывает с
workstation path и key-passphrase credential. Private key bytes не входят в
inventory.

## AskPass

Стандартный OpenSSH mechanism запроса password/passphrase у helper process.
Console запускает password-only mode отдельным процессом `sshfleet` и передаёт secret
через stdout pipe, а не argv.

## Probe

Короткий read-only non-interactive SSH run, собирающий Linux/SSH/system metadata.
Probe не agent: ничего не устанавливает и не сохраняет на host.

## Bounded concurrency

Жёсткий верхний предел одновременно активных probes/commands. Default probes —
`2 × GOMAXPROCS`; очередь не создаёт goroutine на каждый host сверх ceiling.

## Overlay

Private TOML layer для локального rename/tags/routing/probe settings конкретного
`source:alias`. Base source не изменяется.

## Group / command preset

- **Group** — private cross-source selection по explicit members/patterns.
- **Command preset** — trusted local argv + timeout/concurrency, который можно
  запустить по exact confirmed snapshot группы.

## Terminal tab / Preview terminal

- **Terminal tab** — независимый local/SSH/container PTY рядом с постоянной
  Fleet tab; сохраняет final screen и явное состояние завершения.
- **Preview** сохраняет fleet panes и запускает remote shell во встроенном PTY/VT;
  `Ctrl+]` возвращает в приложение.

## Host-key repair

Guarded workflow для реально изменившегося server key: read-only inspection,
independent fingerprint verification, backup-first atomic old-entry removal и
explicit OpenSSH `ask` при следующем connection.

## Capability / healthcheck

Capability — доступность обязательного или optional executable/artifact.
Healthcheck показывает resolved path, origin `sshfleet|system|missing`, impact и
remediation. Missing optional capability скрывает только зависимый menu action.

## Trusted local config

Отдельный строгий локальный TOML source `local_config`. Только он может выбрать
shell, отдельный argv и working directory для direct localhost либо routing к
локальному sshd. Этот schema kind запрещён для remote/encrypted inventory.

## Local container target

Динамическая не-SSH цель, полученная из локального Docker/Podman runtime. Её
identity состоит из allowlisted runtime и immutable container ID; действия —
shell, Preview shell, logs и refresh. Ключи и agent автоматически не передаются.

## Runtime source state

Состояние динамического container source: `loaded`, `empty`, `partial`,
`denied`, `unavailable` или `stale`. `stale` означает, что текущий refresh не
удался, но Console сохранила последний успешный snapshot с явным предупреждением.

## Container shell policy

Явное правило выбора shell только в момент открытия terminal. Текущая policy
`first_available` проверяет абсолютные пути из `shell_priority` по порядку;
discovery и inspect от наличия shell не зависят.

## Owned tool / system tool

Owned tool установлен и проверяется SSH Fleet Console. System tool найден в
обычном PATH. Resolver всегда предпочитает owned, но core не требует optional
tools.

## Portable workspace

Checksummed bundle `lf`, `nvim`, `dtop`, `bat` и reviewed configs, временно
uploaded на совместимый host без sudo/dotfile changes и удаляемый по умолчанию.

## Provenance

Встроенные в binary version, branch, commit, dirty state, source/build date и
channel, позволяющие связать artifact с проверенным source SHA.
