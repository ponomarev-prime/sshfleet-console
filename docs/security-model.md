# Модель безопасности

Цель SSH Fleet Console — добавить inventory, наблюдаемость и UX вокруг OpenSSH,
не ослабляя его security model и не создавая второе хранилище секретов.

## Trust boundaries

| Вход | Доверие | Что разрешено |
|---|---|---|
| `~/.ssh/config` | trusted local code | полный OpenSSH semantics |
| Additional local `ssh_config` | trusted local code | полный OpenSSH semantics |
| Main app TOML / overlays | private local config | app settings, routing, groups, local presets, secret refs |
| Restricted inventory | untrusted data | closed hosts/groups schema, routing/tags/refs |
| Encrypted local bundle | signed confidential data | только restricted inventory после verify/decrypt |
| Remote bundle | authenticated network + signed confidential data | та же restricted schema; executable config запрещён |
| Probe/session output | untrusted remote bytes | bounded sanitized display/data parsing |
| Companion archive | local trusted artifact | closed file shape после checksum validation |

OpenSSH config считается кодом, потому что `ProxyCommand`, `KnownHostsCommand`,
`Match exec` и некоторые вспомогательные directives способны запускать local
programs. Поэтому remote `ssh_config` запрещён даже после шифрования.

## Secrets flow

```text
TOML credential reference
        │ provider + lookup key (не secret)
        ▼
Secret Service / KWallet compatibility API
        │ secret только в памяти password-only helper
        ▼
sshfleet AskPass subprocess stdout pipe
        │
        ▼
OpenSSH prompt input
```

Secret value не должен попадать в:

- command-line arguments;
- TOML/source fragment/overlay;
- environment value;
- application log, Preview или last-session tail;
- test snapshot/fixture, crash report или Git artifact.

Environment содержит только non-secret provider/key references и mode marker.
AskPass отказывается отвечать на host-key confirmation. Private keys остаются
локальными mode-0600 files или в ssh-agent; inventory использует logical identity.

## Restricted и remote source pipeline

Порядок fail-closed:

1. HTTPS response с optional bearer из Secret Service; redirects запрещены.
2. Timeout и maximum byte limit применяются до processing.
3. Manifest связывает source ID, monotonic revision, created/expires и SHA-256
   ciphertext.
4. SSHSIG проверяется по pinned local `allowed_signers` policy.
5. Hash, source ID, expiry и anti-rollback state проверяются до decrypt.
6. age identity получается по local protected reference.
7. Plaintext расшифровывается только в memory и проходит strict closed-schema
   TOML decode.
8. Disk cache обновляется atomically и содержит только manifest, signature и
   ciphertext.

При любой неоднозначности source не загружается. Старый verified cache может
использоваться только в рамках явно реализованной политики; invalid fresh data
не становится trusted автоматически.

## OpenSSH invocation

Аргументы передаются отдельными argv values, без local shell interpolation.
Probe отключает TTY, forwarding, local commands и remote command overrides,
использует non-interactive authentication и подаёт constant script в `sh -s`.
Restricted inventory изолирован от user OpenSSH config через `ssh -F /dev/null`.

Обычная interactive session сохраняет effective OpenSSH behavior и
`known_hosts`. Приложение никогда не устанавливает
`StrictHostKeyChecking=no`.

## Changed host key

Repair состоит из независимых стадий inspection и mutation. До удаления old
entry пользователь обязан проверить presented fingerprint по другому каналу.
Mutation:

- принимает только effective writable `UserKnownHostsFile`;
- запрещает symlink и ambiguous ownership;
- создаёт unique timestamped mode-preserving backup;
- применяет `ssh-keygen -R` к temporary copy;
- обнаруживает concurrent original changes;
- делает atomic replacement;
- на следующем connection принудительно включает `ask` и запрещает UpdateHostKeys.

GlobalKnownHostsFile/KnownHostsCommand исправляются у их владельца, не Console.

## Group commands

Command presets существуют только в trusted local main config. Пользователь
сначала видит immutable host set и exact argv, затем подтверждает. Remote command
получает safely quoted argv; local shell, TTY и forwarding выключены. Concurrency,
timeout и output bounds применяются per run, results разделяются per host.

Это не orchestration/control-plane substitute: privileged массовые операции и
web audit требуют отдельного SSH Fleet Hub contract.

## Portable workspace

Перед upload приложение проверяет checksum, regular archive, allowlisted
structure и traversal/symlink hazards. Remote host проверяется на поддерживаемые
OS/architecture/libc. Workspace создаётся private, получает только binaries и
reviewed configs и удаляется по умолчанию.

Не передаются keys, passwords, tokens, agent sockets, local Docker socket,
dotfiles, SSH config или package-manager authority. Dtop использует только уже
доступный remote Docker socket.

`local_config` является trusted local code: только он может выбирать executable,
argv и working directory для direct localhost. Restricted/encrypted/remote
inventory не может ссылаться на этот schema kind. Container targets берутся
только из локального Docker/Podman `ps` и read-only batched `inspect`; перед
`inspect/exec/logs` immutable ID и runtime проверяются до построения argv. Inspect
output bounded и control-cleaned; command metadata маскирует secret-like assignments/flags,
URL credentials и query до Preview. Context/endpoint информационные, URL
userinfo/query удаляются. SSH keys, credentials и agent socket в container не
копируются и автоматически не пробрасываются; `sudo` не используется.

## Untrusted output и устойчивость

Aliases, banners, process names, errors и session bytes могут содержать invalid
UTF-8, ANSI/OSC/control sequences и огромные строки. На каждой границе данные
нормализуются, ограничиваются по size/lines и очищаются перед rendering/logging.
Ошибка одного source, host, editor или optional tool не должна рушить event loop
или портить terminal state.

## Локальные данные

| Данные | Срок/место |
|---|---|
| App config, fragments, overlays | private XDG config, persistent |
| Secret values | desktop Secret Service, не файлы Console |
| Remote verified cache | encrypted-only XDG state/cache |
| Last session | bounded process memory, до завершения процесса |
| Host-key backup | рядом с known_hosts, persistent и явно показан |
| Remote workspace | `/tmp`, removed by default |
| Regression artifacts | repo-local ignored `.artifacts`, могут содержать test-only metadata |

Реальные infrastructure configs/snapshots нельзя коммитить или прикладывать к
public issue. Процесс disclosure описан в [SECURITY.md](../SECURITY.md), а
следующие hardening steps — в [security-backlog.md](security-backlog.md).
