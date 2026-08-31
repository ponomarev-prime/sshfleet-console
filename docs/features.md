# Каталог функциональности

Этот документ фиксирует реализованный пользовательский контракт. Если функция
меняется, соответствующие README/guide/config/security sections и regression
test обновляются в том же commit.

## Inventory и источники

| Функция | Состояние | Проверка |
|---|---|---|
| Built-in `~/.ssh/config` source `user` | реализовано, default on | config/inventory + PTY |
| Literal aliases и recursive `Include` | реализовано | sshconfig unit/system snapshot |
| Repeatable `--ssh-config`, `--inventory` | реализовано | CLI/source tests |
| Trusted `local_config` / `--local-config` | реализовано, direct localhost или local sshd | strict config + inventory + menu |
| Dynamic Docker/Podman targets + read-only inspect | реализовано, default on | runtime unit + real Docker/Podman PTY E2E |
| Persistent `source add` wizard/fragments | реализовано, atomic 0600 | CLI + PTY E2E |
| Strict restricted TOML inventory | реализовано, unknown fields rejected | source suite |
| Signed + age local bundle | реализовано, in-memory decrypt | real age/SSHSIG integration |
| Authenticated HTTPS remote bundle | реализовано, encrypted-only cache | TLS server integration |
| Remote executable `ssh_config` | намеренно запрещено | negative source tests |
| Per-host private overlays | реализовано, base untouched | model + PTY edit/reload |
| Cross-source groups + private `groups.d` keyboard CRUD | реализовано | storage/model + real PTY screenshots |

## Platform foundation и local terminal

| Функция | Состояние | Проверка |
|---|---|---|
| `[terminal] default_shell` + separate `shell_args` | реализовано | strict config + resolver + inventory |
| CLI → TOML → OS auto precedence | реализовано | resolver/CLI tests + healthcheck smoke |
| Linux Unix PTY и local metrics | реализовано | unit + PTY E2E + regression |
| macOS core/path/shell/Unix-PTY foundation | build-tested; native local metrics/Keychain ещё не реализованы | native CI + cross-build |
| Windows core/path/shell foundation | build-tested; ConPTY/Credential Manager ещё не реализованы | native CI + cross-build |

OS adapter централизует platform capabilities, user config/cache/home paths,
executable semantics и shell detection. Missing optional native capability
отключает зависимое действие и объясняется healthcheck; SSH inventory и другие
capabilities продолжают работать. Remote SSH и container shell policies не
наследуют local terminal config.

## Наблюдение за узлами

- Bounded worker pool; default `2 × GOMAXPROCS`.
- Refresh tick не создаёт второй pool: pending sweep стартует после завершения.
- Capacity: logical CPU cores, total RAM, total swap.
- Utilization: CPU delta по двум `/proc/stat` samples и MemAvailable-based MEM%.
- Dtop-style proportional bars, без load-average column.
- Root capacity, latency, uptime, most CPU-active process и age.
- Distribution, kernel/architecture, init, virtualization.
- systemd summary, Docker, containerd, Podman, kubelet.
- Local OpenSSH client отдельно от remote OpenSSH client/server, sshd unit/socket,
  ssh-agent, SSH tools и OpenSSL.
- Distinct waiting/online/network/auth/host-key/Git states.
- Partial/unsupported remote output даёт unknown fields, а не global failure.

Probe использует configured OpenSSH binary, separate argv, no local shell,
non-interactive auth, disabled TTY/forwarding/local commands и constant POSIX
script через `sh -s`. Password AskPass включается только для explicit binding.

## Навигация и rendering

- Responsive flat `lf`-style Sources/Hosts/Preview layout.
- `All available` первым; ниже source rows и отдельная секция `GROUPS`.
- `n` create, host `m` или Enter → `Manage group membership`, group `R` rename,
  `D` delete, `e` edit;
  fragments atomic mode-0600, source configs остаются read-only.
- HOSTS primary by invariant; side-pane shares и nested host name column из TOML.
- CPU%/MEM% используют dtop-style `█/░` bars; neutral selected-row highlight не
  скрывает заполненную и свободную части шкалы.
- Keyboard navigation: Vim keys, arrows, Home/End and panes; `/` performs a
  case-insensitive global multi-term host search across Sources/Groups and
  restores the previous context on clear.
- Context-sensitive dtop-style Enter menu.
- External text bounded, sanitized for invalid UTF-8, controls, ANSI and length.
- Resize-aware PTY; Preview expands temporarily and restores configured layout.
- Narrow terminals degrade to stacked/single-pane modes without panic.

## SSH и действия

| Действие | Поведение |
|---|---|
| Terminal tabs | numbered permanent Fleet + independent SSH/local/container PTY tabs; primary direct `Alt+1…9` plus optional `Ctrl+1…9`, cyclic `Ctrl+N/P`, `Ctrl+G`, immediate local `Ctrl+D` close, auto-return to Fleet after active session exit, terminal-local `q`, confirmed `Ctrl+]` close; bracketed paste is forwarded as one nested-PTY event; installed-launcher PTY regression rejects the legacy foreground banner |
| Preview terminal | embedded real PTY + VT, resize, `Ctrl+]` return |
| Last session | max 12 printable lines, 128 KiB in-memory capture, never persisted |
| Git check | `ssh -T`, no Linux probe/shell assumption |
| Refresh | fleet sweep или selected host action |
| Config edit | application TOML, host overlay, trusted local source config |
| Group command | exact snapshot/argv plan, second confirmation, bounded results |
| Host-key repair | inspect → independent verify → backup → atomic removal → ask |
| Local machine | configured shell in terminal tab/Preview; local probe |
| Local container | discovered shell in terminal tab/Preview, logs tab, refresh |

Container Preview не требует shell и показывает runtime context/endpoint,
immutable ID, image/platform, state/health, entrypoint/cmd, restart policy,
mounts, networks и ports. Runtime sources имеют отдельные состояния `loaded`,
`empty`, `partial`, `denied`, `unavailable`, `stale`; при refresh failure
последний успешный snapshot остаётся видимым как stale. Policy выбора shell —
явная `first_available` по настроенному `shell_priority`.

## Секреты и encrypted identities

- Credential kinds: SSH password, HTTPS bearer, private-key passphrase.
- Provider: Secret Service (GNOME Keyring/KWallet compatibility API).
- Built-in AskPass is a second password-only mode of the same `sshfleet` binary.
- Value goes through helper stdout pipe and never through argv/TOML/log/session.
- Host-key prompts are refused by AskPass and remain explicit terminal actions.
- Logical identity registry maps shareable name to local mode-0600 key path and
  Secret Service passphrase reference.

## Companion tools и workspace

- Resolver order: SSH Fleet-owned executable, then system PATH, never CWD.
- Editor priority default: `nvim`, `vim`, `nano`.
- Healthcheck in TUI and CLI reports path, origin, impact and remediation.
- Core is one `sshfleet` binary; optional full install pins/builds `lf`, `dtop`,
  Neovim and `bat` without committing generated binaries.
- Portable workspace validates checksum and tar shape, supports current
  Linux/x86_64 + glibc 2.34 boundary, uploads by OpenSSH stdin to mode-0700 temp,
  and cleans on normal/error/signal exit by default.
- Enter menu exposes one `Open SSH Fleet workspace` action instead of separate
  tool rows; the temporary shell contains all reviewed companion tools.
- Workspace shell is selected from `$SHELL`, not from a distro name. A temporary
  ENV overlay preserves the user's normal rc environment and then restores the
  bundle PATH for `bash`, `zsh`, `fish` and POSIX shells; dotfiles are not edited.
- `lf` receives reviewed lfrc: Enter opens files through `bat`, paging files over
  50 lines with available `less` or bat's built-in pager; `e` resolves
  `nvim → vim → nano`. Neovim uses
  reviewed init.lua, while dtop only uses remote Docker access already possessed
  by that user.
- Distroless containers without a shell/tar are a separate container-transport
  backlog item; they must not be treated as an SSH workspace.

## Версии и доставка

- `--version`, human `version`, machine-readable `version --json`.
- Dev provenance: branch + 12-char SHA + optional `+dirty` + source date.
- Stable version: strict `vMAJOR.MINOR.PATCH`, main only, clean tree.
- Release workflow separates verify and publish jobs; exact SHA regression,
  public audit, archive metadata and checksums precede publication.
- Core archives: Linux amd64/arm64; optional companion portability remains
  capability-checked rather than promised universally.
- Installer is no-sudo, no-package-manager, no-shell-rc and atomic/versioned.

## Проверяемость

`make regression` — единственная полная команда и release-candidate gate. Она
оставляет timestamped evidence даже при сбое. Короткие watchdog-таймауты
ограничивают зависание unit/coverage/race этапов и сохраняют Go stack trace в
логе соответствующего шага. Remote workspace runner в режиме `keep` заменяет
себя конечной shell через `exec`, поэтому test cancellation адресуется реальному
процессу; режим `cleanup` сохраняет wrapper, владеющий обязательным EXIT-trap.
Shell fixtures запускаются в отдельной process session без controlling TTY и не
могут остановить родительский Fish/Bash job через terminal job control.
Сам regression entrypoint дополнительно вызывается настоящими `sh`, `bash`,
`zsh` и `fish` внутри PTY. Локальный короткий тест пропускает отсутствующую
необязательную shell, но полный regression, CI и release gate требуют и
проверяют все четыре оболочки; probe-mode не запускает полный suite рекурсивно.

Пирамида тестов:

1. **Unit/domain:** strict config, inventory merge, source trust, crypto,
   parsing, sanitization, scheduler, host-key atomicity, buildinfo.
2. **Bubble Tea model/render:** реальные KeyMsg проходят `Update`; semantic View
   проверяется после ANSI stripping.
3. **PTY E2E:** собранный binary получает реальные клавиши; проверяются global search,
   editor suspend/restore, every Enter menu row, две параллельные terminal tabs,
   switch/live-close confirmation, embedded VT, exit и screenshots.
4. **Crypto/network integration:** disposable age/SSHSIG keys и local TLS bearer
   server; hash/signature/expiry/rollback/redirect/size/cache negative cases.
5. **Docker/OpenSSH acceptance:** isolated sshd alias/probe, host-key rotation,
   backup-first repair, portable upload/tool execution/cleanup; отдельные
   настоящие Docker и Podman PTY-проходы inspect/menu/shell/logs/return.
6. **Native foundation:** Linux tests плюс macOS/Windows native platform test и
   core build; local `make cross-build` фиксирует Darwin/Windows compilation.
7. **Release contract:** version injection, dirty state, SemVer gates, workflow,
   archives, checksums, public-secret audit.

Playwright предназначен для будущего `sshfleet-web`; native TUI проверяется Go
model tests и `creack/pty`, поскольку они работают с настоящим Unix terminal.

## Намеренно не реализовано в Console

- собственный SSH protocol stack;
- автоматическое доверие changed host key;
- хранение secret values или private key bytes в inventory;
- remote OpenSSH config/executable directives;
- превращение probes в privileged agent;
- утверждение, что bundled dynamic tools запускаются на любой OS/architecture;
- web UI, subscriptions/control plane и audit server — это будущие отдельные
  `sshfleet-web` и `sshfleet-hub`.

Future product ideas (Foliage collector, host presets, managed Dozzle lifecycle)
закреплены в [product goals](project-goals-and-scenarios.md), но не выдаются за
готовые Console capabilities.
