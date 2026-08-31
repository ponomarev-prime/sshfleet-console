# SSH Fleet Console: цели проекта и пользовательские сценарии

Этот документ — постоянный продуктовый ориентир SSH Fleet Console (`sshfleet`). Перед
изменением поведения, интерфейса, источников, безопасности или тестов решение
нужно сверять с ним. Если продуктовый курс меняется, сначала обновляется эта
заметка, затем код и документация.

## Суть продукта

SSH Fleet Console — терминальный менеджер SSH-ресурсов в стиле `lf` и `dtop`. Он
объединяет обнаружение узлов, быстрый обзор состояния, безопасное подключение и
типовые операции в одном TUI. Главный рабочий столбец — `HOSTS`; `SOURCES` и
`PREVIEW` помогают навигации и объясняют состояние выбранного узла.

Текущий продукт — локальное терминальное приложение на Go и отдельный репозиторий
`sshfleet-console`. Будущие browser UI и subscription backend используют ту же
модель inventory и trust boundaries, но развиваются и выпускаются независимо.

## Семейство продуктов и границы репозиториев

- `SSH Fleet` — зонтичное имя экосистемы и совместимый namespace конфигов,
  credentials, environment variables и протоколов.
- `SSH Fleet Console` / `sshfleet-console` — этот Go TUI. Каноническая команда
  — `sshfleet`; collision-safe alias `sf` создаётся только если имя свободно.
  `sshf` остаётся переходным legacy-alias до `v1.0.0`; `sshfc` и `sfc` не
  вводятся.
- `SSH Fleet Web` / `sshfleet-web` — будущий браузерный клиент. Он не встраивается
  в этот Go module и не выпускается из этого repository workflow.
- `SSH Fleet Hub` / `sshfleet-hub` — будущий server/API: аутентификация,
  подписки на fleets, versioned source delivery и audit. Это отдельный control
  plane, поэтому слово `Control` не используется в имени локальной консоли.
- Интеграция между репозиториями проходит только через versioned schemas,
  OpenAPI/events и подписанные release artifacts. Прямые source imports,
  git-subtree и общий deployable из нескольких репозиториев запрещены.
- Общий контракт сначала описывается и версионируется; реализация каждого
  репозитория обязана fail closed при неизвестной версии контракта.

## Основные цели

1. Быстро находить нужный SSH-алиас среди десятков и сотен ресурсов.
2. До подключения видеть доступность узла, ёмкость и загрузку ресурсов,
   последний активный процесс, сведения об ОС и SSH-стеке.
3. Подключаться в отдельную terminal tab или во встроенный терминал Preview, не теряя
   контекст fleet.
4. Поддерживать несколько источников с явно различающимися уровнями доверия.
5. Не хранить пароли, токены и приватные ключи в TOML, argv, логах или session
   history.
6. Безопасно делиться ограниченным inventory в подписанном и зашифрованном виде.
7. Оставаться совместимым с OpenSSH и существующим `~/.ssh/config`, ключами,
   `ssh-agent`, ProxyJump и `known_hosts`.
8. Быстро опрашивать fleet с ограниченной конкуренцией и без агента на серверах.
9. Делать опасные операции явными, проверяемыми, обратимыми и backup-first.
10. Покрывать критические пользовательские пути автоматическими тестами, включая
    настоящий PTY, снимки итогового экрана, OpenSSH, Docker, `age`, SSHSIG и TLS.
11. Из выбранного SSH-алиаса открывать полноценное удалённое рабочее окружение:
    shell, `lf`, `nvim` и `dtop`, с предсказуемым выбором полного или
    Preview-терминала.
12. Иметь воспроизводимый локальный companion toolchain (`lf`, `dtop`, `nvim`,
    `bat`) с проверенными версиями и общими конфигурационными профилями.
13. В веб-версии запускать типовые диагностические и эксплуатационные действия
    по явно выбранным наборам узлов, с предварительным планом, ограничением
    параллелизма, подтверждением и полным аудитом результата каждого host.
14. Поддержать глубокую инвентаризацию через временный проверенный collector,
    используя Foliage Agent как источник схем и коллекторов, но не перенося в
    SSH Fleet Console его текущую модель root command runner без доработки безопасности.
15. Предлагать управляемую установку дополнительных приложений, начиная с
    Dozzle, только как отдельный versioned lifecycle workflow с plan/apply,
    проверкой предпосылок, rollback и безопасной сетевой конфигурацией.
16. Уже в TUI объединять hosts из любых sources в пользовательские группы и
    запускать по группе локально доверенные command presets с подтверждением,
    bounded concurrency и раздельным результатом каждого host.
17. Выбирать локальные companion tools детерминированно: сначала проверенные
    бинарники SSH Fleet Console, затем системные. Для редактора глобальный fallback
    порядок — `nvim`, `vim`, `nano`; фактический путь и origin видны в
    application healthcheck.
18. Не падать и не повреждать экран из-за invalid UTF-8, control sequences,
    слишком длинных строк, ошибок OpenSSH/редактора/companion tool или частичного
    отказа source; внешний текст всегда bounded и sanitised на своей границе.
19. Иметь одну команду полного regression-прогона, включающего unit,
    race/vet/build, source/security, PTY menu traversal, group actions и Docker
    acceptance; каждый прогон сохраняет logs, coverage и terminal screenshots.
20. Распространяться как небольшой core без `sudo` и модификации shell rc;
    optional companion tools устанавливаются только явным `--full`, а релизы
    воспроизводимо собираются GitHub Actions из защищённой `main`.
21. Работать после копирования одного бинарника `sshfleet`: самостоятельно находить
    обязательный OpenSSH и optional capabilities, безопасно отключать только
    зависимые действия и объяснять состояние через Neovim-подобный healthcheck.
22. Встраивать в каждый бинарник branch, commit, clean/dirty state и source date;
    stable SemVer release из `main` публиковать только после полного regression
    на том же SHA, проверки provenance, checksums и сохранения evidence.
23. Сохранять независимые release cycles и минимальные trust boundaries между
    Console, Web и Hub; совместимость обеспечивать контрактами, а не monorepo.
24. Документировать каждую пользовательскую capability вместе с её trust
    boundary, настройками, сценариями и проверками: README остаётся быстрым
    входом, а versioned guide/config/features/security/glossary являются
    каноническими подробными справочниками и проверяются автоматическим docs test.
25. Показывать собственную машину и работающие локальные Docker/Podman
    containers в общей навигации, но сохранять разные trust boundaries: localhost
    executable выбирается только trusted local config, container ID приходит
    только от runtime discovery, а SSH keys/agent не копируются и не форвардятся.
26. Работать на Linux, macOS и Windows через явные OS adapters для paths,
    PTY/ConPTY, process execution, credential store, editor/tool discovery и
    container runtime, сохраняя один продуктовый и security-контракт.
27. Давать пользователю явный выбор local default shell в application TOML и
    показывать effective shell/capability отдельно от remote SSH shell и shell
    внутри container; аргументы всегда передаются отдельным argv.
28. Разделить левую навигацию на `SOURCES`, `GROUPS` и вычисляемые `VIEWS`, не
    меняя происхождение host при добавлении в private cross-source group.
29. Поддержать несколько независимых terminal sessions во вкладках: постоянную
    Fleet-вкладку и local/SSH/container tabs с безопасным open/switch/close,
    resize и явным состоянием завершения.
30. Показывать read-only container inspection независимо от наличия shell:
    context/endpoint, immutable ID, image, health, command, mounts, networks,
    ports, restart policy и platform; отсутствие доступа к runtime не должно
    выглядеть как пустой список.
31. Публиковать исходники и screenshots только из privacy-reviewed clean
    snapshot: private development history/archive tags и обычные regression
    artifacts не пересекают public boundary; LICENSE/NOTICE, dependency notices,
    repository rulesets и security reporting обязательны до первого release.

## Базовая модель интерфейса

- `SOURCES` — узкий левый столбец. Первая строка всегда `All available`, ниже —
  каждый доступный источник и число узлов.
- `HOSTS` — самый широкий и главный столбец. В заголовке находятся `CPU`, `MEM`,
  `SWAP`, `CPU%`, `MEM%`. Полосы загрузки должны быть длинными и читаемыми,
  близкими по идее к `dtop`.
- `PREVIEW` — правый информационный столбец, достаточно широкий для различимого
  SSH-контекста, status, system и последних строк сессии. Рекомендуемая ширина
  на desktop — 24%; `HOSTS` при этом остаётся главным столбцом.
- Относительная ширина `SOURCES` и `PREVIEW` задаётся в `[app.ui]`; код хранит
  только responsive-ограничения, которые не дают боковым панелям вытеснить
  главный `HOSTS`.
- Интерфейс должен оставаться плоским и компактным, ближе к `lf`, без тяжёлых
  рамок и визуального шума.
- `j/k`, стрелки, `g/G`, `h/l`, `Tab`, `/` и `Esc` обеспечивают клавиатурную
  навигацию и фильтрацию.
- Во время embedded SSH session Preview временно расширяется за счёт `HOSTS`,
  оставляя fleet различимым; resize передаётся remote PTY. После закрытия
  исходные проценты `[app.ui]` восстанавливаются без изменения config.

## Данные узла

В таблице hosts показываются:

- лампочка/состояние доступности;
- имя или алиас;
- число CPU cores;
- общий объём RAM;
- общий объём swap или его отсутствие;
- текущая загрузка CPU и памяти длинными полосами;
- строкой ниже — наиболее активный процесс, PID, state, CPU и возраст данных.

В Preview происхождение данных всегда явно разделено:

- `CONNECTION · LOCAL` — источник и путь к использованному SSH config, а также
  локально вычисленные alias, target, user, port, proxy, identity и
  не-секретная credential binding;
- `SSH CLIENT · LOCAL` — путь и версия OpenSSH-клиента, которым SSH Fleet Console
  запускает соединение на рабочей станции;
- `SSH SOFTWARE · REMOTE` — установленный на выбранном сервере SSH client,
  версия и состояние `sshd`, `ssh.service`/`sshd.service`, SSH socket,
  `ssh-agent`, `scp`, `sftp`, `ssh-keygen`, `ssh-add` и OpenSSL;
- `HOST STATUS · REMOTE` — CPU, memory, swap, uptime, root filesystem и top
  process;
- `SYSTEM · REMOTE` — ОС, kernel, architecture, init, virtualization, systemd,
  Docker, containerd, Podman и kubelet;
- `LAST SESSION · REMOTE` — последние несколько очищенных строк предыдущей
  SSH/Git-сессии.

## Источники и их границы доверия

### 1. Встроенный пользовательский OpenSSH config

- `~/.ssh/config` загружается по умолчанию как source `user`.
- `--no-user-ssh-config` полностью отключает его для изолированного запуска.
- `--user-ssh-config` временно включает его поверх TOML-настройки.

### 2. Дополнительный OpenSSH config

- Подключается через TOML, `sources.d`, `source add` или повторяемый
  `--ssh-config`.
- Считается доверенным локальным кодом: OpenSSH directives могут выполнять
  программы.
- Хранит алиасы и ссылки на `IdentityFile`, но SSH Fleet Console не копирует приватные
  ключи.
- Удалённый `ssh_config` запрещён.

### 3. Restricted inventory TOML

- Содержит только hosts, groups, tags, routing metadata, probe policy и ссылки
  на credentials.
- Пароли, токены, shell snippets, `ProxyCommand`, `Match exec` и неизвестные
  поля запрещены.
- Подключения из такого inventory всегда изолированы через `ssh -F /dev/null`.
- Зашифрованные OpenSSH private keys остаются локальными файлами mode `0600`.
  Shareable inventory содержит только логическое имя identity; локальный main
  config связывает его с путём и `key-passphrase` ссылкой на Secret Service.

### 4. Локальный encrypted inventory

- Использует bundle `manifest.toml`, `manifest.sig`, `inventory.toml.age`.
- Manifest связывает source ID, revision, created/expires и SHA-256 ciphertext.
- SSHSIG проверяется по локальному `allowed_signers`.
- `age` identity берётся из Secret Service или явно разрешённого plugin identity.
- Расшифровка выполняется только в памяти, после проверок подписи, hash, expiry и
  anti-rollback.

### 5. Remote inventory

- Использует тот же restricted encrypted bundle по HTTPS.
- Bearer token хранится отдельной ссылкой Secret Service.
- Redirect запрещён, размер и время ограничены, cache содержит только ciphertext,
  manifest и signature.
- Remote OpenSSH config и исполняемые поля запрещены без исключений.

## Секреты

- Пароли SSH и bearer tokens хранятся в Secret Service/KWallet.
- TOML хранит только provider, credential name и lookup key.
- Пароль передаётся OpenSSH через закрытый pipe отдельного AskPass-процесса. В
  официальном core это второй процесс того же `sshfleet`; отдельный helper допустим
  только как совместимый явно проверяемый override.
- Секретные значения не должны попадать в argv, environment values, TOML, логи,
  Preview, session tail, crash reports или test fixtures.
- Приватные ключи и passphrases остаются под управлением OpenSSH/`ssh-agent`.

## Настройки приложения

Постоянные настройки поведения приложения находятся в едином `[app]` TOML:

- refresh interval;
- connect timeout;
- `max_concurrent`;
- включение probe;
- загрузка пользовательского SSH config;
- пути `sources.d`, overlays и source state/cache;
- editor, SSH binary;
- приоритеты локальных tools и редакторов;
- cross-source groups и локально доверенные command presets;
- portable workspace bundle and cleanup policy;
- лимиты и timeout удалённых источников.
- `app.ui.sources_width_percent`, `app.ui.preview_width_percent` и
  `app.ui.host_column_percent`.

Если `max_concurrent` не задан, используется `2 × GOMAXPROCS`. CLI-флаги имеют
приоритет на один запуск. Опечатки и неизвестные поля должны завершать запуск
ошибкой, а не молча менять поведение.

## Application healthcheck и выбор tools

- Для каждого tool resolver сначала проверяет принадлежащие SSH Fleet Console launchers
  и pinned binaries, затем системный `PATH`; current working directory не влияет
  на выбор.
- Редактор выбирается по глобальному порядку `nvim → vim → nano`, если CLI или
  TOML не задали более узкий явный приоритет. Значение всегда является именем
  executable без shell arguments.
- Healthcheck показывает секции core/credentials/sources/editing/workspace,
  уровень `OK|WARN|FAIL|INFO`, resolved absolute path,
  `sshfleet|system|missing` origin, влияние отсутствия capability и подсказку.
  Секреты и полный environment туда не попадают.
- Обычный healthcheck завершается с ошибкой только при отсутствии обязательной
  capability; `--strict` также считает предупреждения ошибкой для CI и полной
  workstation-проверки. `doctor` и `checkhealth` являются алиасами команды.
- Отсутствующий optional tool отключает только своё действие. Отсутствующий
  editor оставляет навигацию и SSH рабочими, но объяснимо блокирует edit action.

## Локальные группы и group commands

- Пользовательская группа живёт в основном private TOML и содержит стабильные
  `source:alias` members и/или явные alias patterns. Поэтому она объединяет hosts
  из любых OpenSSH, restricted, encrypted и remote sources, не модифицируя их.
- Один host может входить в несколько групп. Группа появляется рядом с sources
  как virtual filter; membership всегда показывается в Preview.
- Command presets также находятся только в доверенном локальном app config.
  Remote/restricted source не может добавлять executable, arguments или shell
  text.
- Preset задаёт непустой argv, timeout и concurrency. SSH Fleet Console передаёт argv с
  безопасным POSIX quoting, запрещает control bytes и не подставляет secrets.
- Запуск по группе всегда сначала показывает точный target snapshot и команду,
  затем требует подтверждение. Результат каждого host содержит exit state,
  duration и bounded sanitised tail; ошибка одного host не останавливает UI и
  не скрывает результаты остальных.

## Локальный companion toolchain

- Исходники `lf`, `dtop`, Neovim и `bat` подключены как Git submodules и
  закреплены точными release commit SHA; версии, URL и SHA продублированы в
  проверяемом `tools/manifest.toml`.
- Локальные эксперименты выполняются в ветках `sshfleet` внутри submodules. До
  публикации изменённый commit обязан быть отправлен в доступный remote fork,
  иначе основной репозиторий не должен ссылаться на недостижимый gitlink.
- Общие настройки находятся в отслеживаемых `tools/config/*`. Launchers из
  `tools/bin/*` выбирают эти настройки явно и не перезаписывают пользовательские
  `~/.config`.
- Бинарники, Cargo/CMake build trees и установленный Neovim runtime находятся в
  `.toolchain/` и никогда не отслеживаются Git.
- `dtop` собирается без self-update. Обновление любого инструмента — отдельное
  reviewable изменение submodule + manifest, после которого выполняется smoke.
- Профили companion toolchain не содержат SSH credentials, private keys,
  bearer tokens или произвольные команды, полученные из remote source.
- Обычный пользователь запускает только `sshfleet`. Launcher сам предоставляет
  приложению companion PATH и AskPass; shell activation остаётся исключительно
  developer-инструментом и не является частью пользовательского пути.
- Launcher определяет своё реальное расположение независимо от текущего
  каталога и цепочки symlink; один и тот же запуск через `PATH` поддерживается в
  POSIX `sh`, Bash, Zsh и Fish без изменения shell rc.
- Обычная установка и release archive содержат один self-contained binary
  `sshfleet`; shell-neutral launchers дают каноническую команду и переходный
  `sshf`. Alias `sf` создаётся installer только при отсутствии конфликта.
  Отдельный compatibility AskPass не обязателен. `--full` по явному выбору
  инициализирует официальные pinned submodules и собирает companion toolchain;
  недоступный optional tool не оставляет неработающий пункт действия.
- Установка пишет новую versioned directory, а затем атомарно переключает
  `current`; неуспешная сборка или копирование не повреждает рабочую версию.
- `dev` является integration branch, `main` — защищённой release branch.
  Development version имеет вид `branch-shortSHA[+dirty]`; stable release —
  возрастающий `vMAJOR.MINOR.PATCH`. Только manual main-only workflow после
  полного regression создаёт тег и GitHub Release последним шагом.

## Отложенный веб-контур автоматизации

Эти возможности относятся к этапу веб-приложения. Текущий TUI остаётся
agentless SSH navigator и не получает неограниченный удалённый command runner.

### Command presets и host sets

- `host set` — именованный, предварительно вычисляемый набор целей по source,
  group, tag и alias pattern. Перед запуском UI показывает точный immutable
  snapshot узлов; изменение inventory не расширяет уже подтверждённый job.
- `command preset` — versioned описание действия с фиксированным executable,
  типизированными аргументами, timeout, требуемыми capabilities, ожидаемыми exit
  codes, лимитами stdout/stderr и признаком `read-only|mutating|destructive`.
- Restricted и remote inventory могут ссылаться только на известный ID preset,
  но не определять shell text. Новые executable и шаблоны команд поступают из
  доверенного локального policy repository и проходят review/signature check.
- Произвольный `/bin/sh -c` не является обычным preset. Отдельный
  `local-trusted` escape hatch допустим только для администратора и всегда
  помечается как произвольное выполнение кода.
- Выполнение двухфазное: `plan` показывает targets, resolved arguments,
  privilege level и возможные изменения; `apply` требует подтверждение. Для
  mutating/destructive действий веб-версия поддерживает approval policy.
- Scheduler ограничивает глобальную и per-host конкуренцию, поддерживает
  cancel, не запускает повторно non-idempotent действие автоматически и хранит
  по каждому узлу exit status, duration и очищенный bounded output.
- Секреты передаются только через credential binding/AskPass и никогда не
  подставляются в command template, argv, audit log или результат job.
- Для сложного configuration management preset может быть адаптером к
  закреплённому Ansible playbook. `--check`, `--diff`, `--list-hosts` и
  `--limit` используются там, где соответствующие modules это поддерживают;
  simulation не считается доказательством полной idempotency.

### Foliage deep inventory

- Исследованный Foliage Agent — статический Go collector и NATS publisher с
  полезными схемами `ipaddr`, `lsblk`, `dmidecode`, `lshw`, OS, packages,
  services, accounts, cluster и storage data. Его текущая реализация также
  запускает конфигурационные команды от root, не имеет command timeout, durable
  delivery и достаточного sandboxing, поэтому не устанавливается автоматически
  в production в неизменённом виде.
- Первый безопасный вариант — временный one-shot `Collect full inventory`:
  SSH Fleet Console проверяет подписанный binary/collector manifest, загружает его в
  mode-0700 temporary directory, запускает non-root allowlisted collectors,
  принимает bounded versioned JSON по существующему SSH transport и очищает
  workspace.
- Root-only секции (`dmidecode`, часть `lshw`, cluster/storage tooling) требуют
  отдельного elevated profile, точечного sudo allowlist и явного подтверждения.
  Отсутствие прав даёт `partial` result, а не попытку тихо расширить привилегии.
- По умолчанию временный collector не получает NATS credentials и не открывает
  listener. Постоянный агент возможен позже только после появления устойчивой
  host identity, mTLS, signed config, command allowlist, timeout/cancellation,
  non-overlap, spool/ack/retry, sandboxed systemd unit и безопасной ротации.
- Full inventory имеет schema/version, timestamp и статус каждой секции.
  Домашние каталоги, SSH keys, environment secrets, password hashes, содержимое
  файлов и полные process arguments не собираются; чувствительные поля проходят
  classification/redaction и ограниченную retention policy.

### Управляемая установка Dozzle

- `Install Dozzle` — не raw command preset, а отдельный lifecycle workflow:
  `detect → plan → apply → verify → status → upgrade → uninstall/rollback`.
- Plan проверяет Docker/Podman, доступ к daemon, architecture, уже существующий
  Dozzle, занятость портов, способ systemd/Compose/Quadlet и показывает все
  создаваемые файлы, container privileges, mounts и network exposure.
- Image закрепляется version и digest. Существующие конфиги сначала копируются
  в backup; повторный apply обязан быть idempotent, а uninstall удаляет только
  объекты с ownership labels SSH Fleet Console.
- По умолчанию UI/agent не публикуются во внешнюю сеть: используется loopback и
  SSH tunnel либо private network. Для внешнего agent endpoint нужны собственные
  сертификаты и аутентифицированный Dozzle control plane.
- Доступ Dozzle к Docker API считается эквивалентным высокой привилегии.
  Shell/actions выключены по умолчанию; публичный unauthenticated UI и открытый
  Docker TCP socket запрещены. Ограниченный socket proxy предпочтителен для
  read-only UI, но его совместимость с Dozzle agent mode проверяется отдельно.

Исходные материалы разведки: внутренние заметки reverse engineering, не входящие
в публичный репозиторий, и официальные документы
[Dozzle Agent Mode](https://dozzle.dev/guide/agent),
[Dozzle Authentication](https://dozzle.dev/guide/authentication) и
[Ansible check/diff mode](https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_checkmode.html).

## Основные пользовательские сценарии

### A. Обычный запуск

1. Пользователь запускает `sshfleet`.
2. Загружается `~/.ssh/config` и постоянные дополнительные sources.
3. Появляется `All available`, начинается ограниченный параллельный probe.
4. Пользователь фильтрует список, выбирает узел и изучает Preview.

### B. Изолированный custom SSH config

1. Запуск: `sshfleet --no-user-ssh-config --ssh-config NAME=PATH`.
2. Алиасы и wildcard-правила из `~/.ssh/config` не загружаются и не влияют на
   OpenSSH resolution.
3. В Preview видны выбранный source и точный config path.

### B1. Localhost из trusted local config

1. Пользователь создаёт отдельный `local_config` с `mode = "direct"`, shell,
   отдельным `shell_args` и optional working directory либо с `mode = "ssh"`,
   host/port/user и optional shell на локальном sshd.
2. Source подключается через main TOML, `sshfleet source add --type local` или
   repeatable `--local-config`; remote/encrypted inventory не может выбирать
   executable или working directory на рабочей машине.
3. Direct localhost получает local probe и два первых действия: shell tab и
   shell в Preview; закрытие возвращает исходные размеры fleet.

### B2. Работающий локальный container

1. При включённом `[app.containers]` приложение каждые `refresh_interval`
   вызывает allowlisted Docker/Podman CLI и показывает каждый running container
   как динамический source/target со стабильным runtime+immutable-ID.
2. Enter предлагает container shell в terminal tab, shell в Preview,
   follow последних 200 log lines и refresh. Shell выбирается из локально
   настроенного абсолютного priority list проверкой `test -x` внутри container.
3. Пресеты групп выполняются через runtime `exec` с отдельным argv; приложение
   не вызывает `sudo`, не монтирует ключи, не форвардит agent и не меняет daemon.
4. Исчезновение container обновляет только dynamic targets и не меняет индексы
   во время активного probe/group run.

### C. Выбор источника

1. Фокус переходит в `SOURCES`.
2. `All available` показывает объединённый fleet.
3. Выбор source фильтрует hosts без потери поиска и selection safety.

### D. Меню действий по Enter

1. Пользователь нажимает `Enter` на host.
2. Для обычного SSH host первые два пункта — `Open terminal tab (default)` и
   `Open terminal in Preview`; отдельная PTY tab является безопасным выбором по
   умолчанию.
3. Остальные строки появляются только по контексту: remote workspace, refresh,
   edit overlay, Git check или host-key repair.
4. `j/k` выбирают строку, `Enter` выполняет, `Esc/h/q/Ctrl+C` закрывают меню.

### E. SSH terminal tab

1. Постоянная Fleet tab остаётся доступна, а OpenSSH получает независимый PTY.
2. Нумерация начинается с `1:Fleet`; основной `Alt+1…9` или дополнительный
   `Ctrl+1…9` выбирает слот напрямую, `Ctrl+N`/`Ctrl+P` циклически переключают
   сессии, `Ctrl+G` возвращает Fleet.
3. Normal exit активной сессии автоматически возвращает во Fleet, сохраняя
   final screen и явный `✓` в завершённой вкладке; `Ctrl+]` закрывает finished tab.
4. Живой foreground process закрывается только повторным `Ctrl+]`; `Esc` отменяет.
5. Последние очищенные строки вывода появляются в Preview.
6. Bracketed paste остаётся единым событием и передаётся вложенному PTY, не
   интерпретируясь как локальные сочетания переключения/закрытия вкладок.
7. `q` и остальные обычные клавиши внутри живой terminal tab принадлежат
   вложенному PTY и не завершают SSH Fleet Console.
8. `Ctrl+D` — явное локальное быстрое закрытие: активная вкладка удаляется и
   Fleet показывается до ожидания остановки PTY; это не зависит от обработки EOF
   вложенным shell/TUI.

### F. SSH внутри Preview

1. Fleet остаётся видимым, все обычные клавиши передаются удалённому PTY.
2. `Ctrl+]` локально закрывает embedded terminal.
3. Resize изменяет remote PTY и не ломает layout.

### G. Git endpoint без shell

1. Linux probe распознаёт стандартный Git no-shell endpoint.
2. Узел получает состояние Git access, а не ошибку Linux shell.
3. Меню предлагает foreground `ssh -T` check.

### H. Password alias (`password-node-*`)

1. Host rule связывает pattern с credential name.
2. Значение сохраняется через `sshfleet credential set NAME` в Secret Service.
3. Probe и OpenSSH получают пароль через отдельный AskPass-процесс того же
   `sshfleet` (либо совместимый явно настроенный helper).
4. Ошибка аутентификации не раскрывает пароль и видна в Preview.

### I. Редактирование алиаса без изменения исходника

1. Пользователь выбирает `Edit host overlay`.
2. Редактируется отдельный TOML overlay с mode `0600`.
3. Исходный SSH config или shared inventory не изменяется.
4. После reload selection/filter не приводят к panic, изменения сразу видны.

### J. Host key changed

1. SSH Fleet Console показывает новый и сохранённый fingerprints.
2. Пользователь проверяет fingerprint вне SSH Fleet Console.
3. Repair сначала создаёт backup `known_hosts`, затем удаляет точную запись.
4. Следующее подключение использует native prompt OpenSSH.

### K. Shareable encrypted inventory

1. Restricted plaintext inventory проходит строгий parser.
2. `sshfleet source pack` шифрует его и подписывает manifest.
3. Пользователь передаёт только трёхфайловый bundle.
4. Получатель закрепляет allowed signer и предоставляет age identity.
5. Tamper, expiry, rollback или неизвестное поле отклоняют весь source.

### L. Remote source

1. SSH Fleet Console получает bearer token из Secret Service.
2. По HTTPS скачиваются manifest, signature и ciphertext.
3. Выполняются те же проверки и in-memory decrypt, что для local bundle.
4. Ошибка source не добавляет частично проверенные hosts.

### M. Навигация SSH aliases → remote workspace (`lf`/`nvim`/`dtop`)

1. При запуске SSH Fleet Console загружает разрешённые локальные OpenSSH configs и
   inventory sources, затем показывает их перечислимые aliases как точки входа.
2. Пользователь выбирает alias и через Enter-меню запускает обычный shell либо
   единый `Open SSH Fleet workspace`; внутри workspace доступны `lf`, `nvim`,
   `dtop` и `bat`, а выход возвращает в SSH Fleet Console.
3. Workspace открывается в отдельной terminal tab; следующий этап добавляет
   явный вариант в Preview. Обе ветки обязаны использовать тот же OpenSSH alias,
   credential binding, ProxyJump и host-key policy.
4. Явное действие `bundled workspace` передаёт закреплённые `lf`, `nvim`, `dtop`
   и `bat` вместе с отслеживаемыми SSH Fleet Console-конфигами в приватный временный
   каталог цели. Это не установка: `sudo`, package manager и пользовательские
   dotfiles цели не изменяются.
5. Перед загрузкой проверяются platform/architecture, локальный bundle и его
   manifest. Инструменты запускаются только через сгенерированные нами wrappers;
   произвольный локальный каталог или команда в bundle не принимаются.
6. В `lf` Enter на файле использует `bat`: до 50 строк без pager, свыше 50 —
   через доступный `less` либо встроенный pager bat; `e` разрешает редактор как
   `nvim → vim → nano`, поэтому
   bundled `nvim` имеет приоритет. `dtop` работает с Docker socket самой цели.
   SSH Fleet Console не пробрасывает локальный Docker socket, agent или credentials
   и не повышает привилегии; отсутствие доступа к daemon видно как обычная
   диагностируемая ошибка.
7. Remote workspace по умолчанию удаляет весь временный каталог при штатном
   выходе, ошибке или сигнале. Явный debug-режим может сохранить bundle; следующий
   запуск также имеет право очищать только собственные устаревшие каталоги.
8. Локальные настройки передаются только из versioned allowlist внутри bundle с
   отдельным временным `XDG_CONFIG_HOME`; произвольный локальный каталог не
   копируется.
9. Локальные private keys, пароли, bearer tokens, socket `ssh-agent`, history и
   полный `~/.ssh/config` на цель не копируются. Agent forwarding выключен по
   умолчанию.
10. Переход с цели к следующим aliases использует её собственный доверенный
   `~/.ssh/config` либо отдельно сформированный restricted alias profile без
   `ProxyCommand`, `Match exec` и секретов; это отдельное явное разрешение, а не
   побочный эффект запуска `lf`.

### N. Сборка companion tools

1. При подготовке выполняется `make app-ready`; Git получает только закреплённые
   исходные commit SHA из официальных или наших fork URL.
2. Target собирает `lf`, `dtop`, Neovim и `bat` без `sudo`, помещает результат с
   Neovim runtime в игнорируемый `.toolchain/`, затем запускает smoke и PTY/config
   verification.
3. `tools/bin/*` запускают эти бинарники с отслеживаемыми конфигами; `batcat`
   остаётся совместимым алиасом для Ubuntu.
4. `make toolchain-smoke` сверяет source URL/SHA, наличие конфигов и launchers,
   затем запускает version checks всех готовых бинарников.
5. После подготовки пользователь вводит только `sshfleet`; launcher доступен через
   `~/.local/bin`, не меняет shell rc и внутренне связывает приложение с
   проверенными companion binaries; встроенный AskPass не требует соседнего
   helper-файла.

### O. Запуск command preset по host set (web)

1. Пользователь выбирает versioned preset и именованный host set.
2. Web backend сохраняет immutable target snapshot и строит план без выполнения.
3. UI показывает targets, resolved non-secret arguments, privilege, concurrency,
   timeout и класс риска; mutating/destructive job проходит подтверждение или
   approval.
4. Выполнение идёт через ограниченный scheduler. Отмена останавливает новые
   старты и пытается завершить уже запущенные процессы в пределах timeout.
5. Итог хранит status и bounded redacted output отдельно для каждого host;
   partial failure не маскируется общим зелёным статусом.

### P. Временный полный сбор Foliage data (web)

1. Пользователь выбирает `Collect full inventory` и видит список секций и
   требуемых прав.
2. Backend проверяет platform, signature и collector manifest, затем загружает
   временный one-shot bundle через существующий OpenSSH trust path.
3. Non-root секции выполняются по умолчанию; elevated секции запускаются только
   после отдельного подтверждения и через узкий sudo policy.
4. Versioned JSON валидируется, нормализуется и показывает `ok`, `partial`,
   `unsupported` или `error` для каждой секции.
5. Temporary bundle удаляется; host не получает NATS credential или постоянный
   service. Повторный сбор показывает diff с предыдущим разрешённым snapshot.

### Q. Установка Dozzle (web)

1. Пользователь открывает host action `Install Dozzle`; первый запуск всегда
   является read-only detect/plan.
2. План показывает pinned image digest, runtime, mounts, Docker API privileges,
   bind addresses, certificates/authentication и rollback path.
3. После approval backend создаёт backup, применяет declarative unit/Compose или
   Quadlet и проверяет health без открытия публичного порта по умолчанию.
4. Web UI показывает способ подключения: SSH tunnel/private agent endpoint и
   явно предупреждает о Docker socket privilege.
5. Upgrade повторяет plan/approval; rollback восстанавливает backup, uninstall
   удаляет только ресурсы, созданные и помеченные SSH Fleet Console.

### R. Application healthcheck и редактирование config

1. Пользователь открывает `?` или запускает `sshfleet healthcheck`/`sshfleet doctor` и
   видит core, AskPass, Secret Service, source crypto, editor, optional tools и
   workspace: путь, origin, влияние отсутствия и подсказку.
2. При наличии SSH Fleet Console tool он имеет приоритет над одноимённым системным;
   отсутствие bundled tool безопасно переводит resolver к system `PATH`.
3. `c` открывает основной app TOML, `e` — private host overlay, а контекстное
   действие — локальный source config. Используется единый editor resolver.
4. Ошибка или non-zero exit редактора показывается в footer/healthcheck и не
   завершает SSH Fleet Console; reload применяется только после успешной строгой parse.
5. Один скопированный `sshfleet` проходит healthcheck и парольный AskPass без
   соседнего helper; отсутствие optional tool скрывает только связанный action.

### S. Cross-source group и запуск preset

1. Пользователь объединяет стабильные host IDs из разных sources в private
   группу и выбирает её как virtual source.
2. Action `Run command preset` показывает доступные локальные presets.
3. После выбора UI показывает command argv, точный список hosts, timeout и
   concurrency; первый Enter только строит plan, второй подтверждает запуск.

### T. Core и full installation

1. `./install.sh` без `sudo` устанавливает versioned однобинарный core `sshfleet` и
   атомарно переключает пользовательский launcher.
2. `./install.sh --full` на Linux/x86_64 дополнительно собирает pinned upstream
   toolchain и remote workspace bundle; исходники и бинарники не коммитятся.
3. Сбой сборки или копирования оставляет прежний `current` рабочим. Новый core
   не показывает bundled actions, отсутствующие system tools видны как missing.
4. Release archive устанавливается тем же скриптом с `--prebuilt`; checksum
   публикуется рядом с архивом.
5. Выполнение ограничено scheduler; прогресс и per-host status остаются видны,
   partial failures не закрывают TUI.
6. Output очищается от ANSI/control/invalid UTF-8, ограничивается по размеру и
   доступен в Preview/result view без попадания secrets в artifacts.

### U. Расширяемый Preview terminal

1. При открытии SSH в Preview правый pane временно становится шире, а HOSTS
   остаётся видимым и пригодным для ориентации.
2. Remote PTY сразу получает новые dimensions и последующие terminal resize.
3. `Ctrl+]`, normal exit и startup error закрывают embedded mode и возвращают
   исходные configured pane percentages без накопления layout drift.

### V. Полная regression-команда и artifacts

1. Одна documented make-команда создаёт новый timestamped artifact directory.
2. В него попадают machine-readable manifest, environment/tool health без
   secrets, logs каждого этапа, coverage report и PTY screenshots.
3. Suite проходит unit, race, vet, build, source/security, model/menu, real PTY,
   group-command fixtures и disposable Docker SSH acceptance.
4. Ошибка любого шага даёт non-zero exit, но уже созданные artifacts не
   удаляются и содержат достаточно данных для воспроизведения.
5. Каждый Go stage ограничен явным watchdog-таймаутом; shell/workspace fixtures
   не оставляют дочерние процессы с унаследованными pipes, а timeout log
   содержит stack trace зависшего теста.
6. Пользователь может вызвать единую команду `make regression` из `sh`, `bash`,
   `zsh` или `fish`; настоящий PTY shell-matrix входит в этот же gate, не крадёт
   controlling terminal и сохраняет отдельный status/log в artifacts.

### W. Версия бинарника и stable release

1. `sshfleet --version` показывает branch-derived ID и короткий SHA;
   `sshfleet version --json` показывает полный commit, channel, clean/dirty state,
   source date, Go и platform без обращения к Git.
2. Локальная сборка с незакоммиченными файлами всегда получает `+dirty` и не
   может быть принята stable release gate.
3. Stable версия имеет строгий вид `vMAJOR.MINOR.PATCH`, всегда относится к
   текущему чистому `origin/main` и обязана быть больше последнего stable tag.
4. Release workflow сначала выполняет полный regression/public audit и сборку
   обеих архитектур на одном SHA, сохраняет evidence и checksums, и лишь затем
   атомарно создаёт tag/GitHub Release. Повторное имя версии запрещено.

### X. Local default shell и native OS adapter

1. В `[terminal]` пользователь выбирает `default_shell = "auto"` либо явный
   executable/logical shell и отдельный `shell_args` argv.
2. Precedence фиксирован: one-shot CLI override → application TOML →
   OS auto-detection. Effective значение и origin видны в Preview/healthcheck.
3. Отсутствующая shell не вызывает скрытый fallback: действие блокируется с
   объяснением и перечнем обнаруженных вариантов.
4. Paths, PTY/ConPTY, process signals, OpenSSH, credential store и Docker
   проходят через OS adapter; Unicode и пробелы в путях не меняют argv.

### Y. Read-only container inspection

1. Runtime adapter показывает выбранный Docker/Podman context/endpoint и явно
   различает `available`, `denied`, `unavailable`, `stale` и пустой runtime.
2. `inspect` использует immutable container ID и показывает metadata без exec,
   поэтому работает для stopped, distroless и scratch containers без shell.
3. Interactive `exec` доступен только running container после capability-check
   выбранной shell. Несколько найденных shell разрешаются явной policy/выбором.
4. Будущий `docker cp` bootstrap является отдельной mutating операцией:
   provenance/checksum, compatibility, exact plan, confirmation и cleanup;
   package install, privileged mode и mount Docker socket не выполняются скрыто.

### Z. Навигация Sources, Groups и Views

1. Левая панель имеет отдельные секции `SOURCES`, `GROUPS`, `VIEWS`; каждая
   строка показывает count и состояние `loaded|loading|stale|partial|error`.
2. Host может входить в несколько плоских private groups; membership изменяет
   только local overlay и никогда не переписывает исходный source.
3. Stable identity остаётся `source:alias`; duplicate aliases, missing members
   и unavailable source показываются явно, а не объединяются молча.
4. Group action по-прежнему сначала фиксирует exact target snapshot и plan,
   затем требует подтверждение.

### AA. Terminal tabs

1. Постоянная вкладка `Fleet` соседствует с несколькими local, SSH и container
   sessions; каждая имеет уникальный runtime `session_id` и target reference.
2. Пользователь может открыть новую tab, перейти следующая/предыдущая/по
   списку, вернуться во Fleet и закрыть session с предупреждением о живом
   foreground process.
3. Input/output/resize изолированы между PTY/ConPTY; завершение одной вкладки
   не влияет на остальные, а disconnected/error/exit code остаются видимы.
4. Tab metadata не содержит passwords, passphrases, private keys, tokens или
   shell history; restore допускает только безопасный non-secret context.

## Правила безопасного поведения

- Никогда автоматически не отключать host-key checking и не принимать host key.
- Не исправлять `known_hosts` без backup и точного плана изменения.
- Не выполнять remote `ssh_config` или команды из restricted inventory.
- Не наследовать `~/.ssh/config` для restricted/encrypted/remote hosts.
- Не выполнять неограниченный fan-out: probes проходят через scheduler.
- Ошибка одного source не должна скрывать корректные sources или рушить TUI.
- Selection использует стабильный host ID, а не случайный индекс после reload.

## Критические автотесты

Каждое изменение должно сохранять зелёными релевантные проверки:

```sh
make check
make test-platform
make cross-build
make test-docs
make test-menu
make test-version
make test-sources
make test-race
make test-e2e
make test-screenshots
make test-docker
make test-workspace-docker
make cover
make toolchain-check
make toolchain-smoke       # после изменения исходников/версий toolchain
make toolchain-ready       # полная повторяемая сборка и acceptance verification
make app-ready             # подготовка единственной пользовательской команды sshfleet
```

Особенно обязательны: все строки action menu через key events; независимые PTY
tabs, editor suspend/restore и embedded terminal; снимки настоящего
PTY с проверкой пропорций панелей и проходом по Enter-меню; bounded scheduler;
все шесть source kinds;
strict inventory rejection; настоящий age+SSHSIG; TLS bearer flow; tamper,
expiry, rollback, redirect и size limits; отсутствие plaintext/credentials в
cache, logs и argv; backup-first repair на disposable Docker sshd.

## Актуальный план развития

Порядок учитывает дополнения из Outline и уже реализованный localhost/container
MVP. Каждый этап завершается отдельным reviewable commit, обновлением
документации и regression evidence; следующий этап не расширяет trust boundary
предыдущего скрытым образом.

### Этап 0 — закрыть текущий localhost/container слой

**Завершён 2026-08-30.** Реализация и доказательства входят в один regression
contract: unit/model tests, `make test-container-docker`,
`make test-container-podman` и PTY screenshots в `.artifacts`.

1. Добавить read-only `docker/podman inspect` Preview без требования shell:
   context/endpoint, immutable ID, image, health, entrypoint/cmd, mounts,
   networks, ports, restart policy, OS/architecture.
2. Различать `runtime empty`, `daemon unavailable`, `access denied`, `stale` и
   stopped container; динамический refresh не должен скрывать последнюю
   объяснимую ошибку.
3. Показывать effective container shell и способ её выбора; если кандидатов
   несколько, использовать явную policy/меню, а не молчаливый fallback.
4. Добавить disposable Docker и Podman PTY acceptance для shell, Preview, logs,
   resize, исчезновения target и cleanup; сохранить screenshots/artifacts.

Фактический результат: batched read-only inspect работает без container shell;
source states различают loaded/empty/partial/denied/unavailable/stale; refresh
сохраняет last-known target как stale; stopped target не предлагает shell;
policy `first_available` и effective shell видимы; настоящие Docker и Podman
acceptance проходят меню, embedded shell, logs, Ctrl+C и возврат в TUI.

### Этап 1 — cross-platform foundation и local shell contract

**Foundation slice завершён 2026-08-30; native adapters продолжаются.** Core
теперь имеет явный `internal/platform` boundary для OS capabilities,
config/cache/home paths, executable semantics и local shell resolution.
`[terminal] default_shell`/`shell_args`, precedence CLI → TOML → OS auto,
effective origin в Preview/healthcheck, fail-closed explicit shell и наследование
только trusted direct-local targets реализованы и покрыты тестами. Linux
сохраняет Unix PTY/local metrics; macOS и Windows проходят native CI core build
и local cross-build. ConPTY, Keychain/Credential Manager и native macOS/Windows
local metrics пока честно отмечаются unavailable и не эмулируются Unix-кодом.

1. Ввести внутренние OS adapters для paths, PTY/ConPTY, process lifecycle,
   signals, terminal capabilities, OpenSSH, editor/tool discovery, credential
   store и container runtime.
2. Добавить `[terminal] default_shell = "auto"` и `shell_args = []`, явный
   precedence CLI → TOML → OS detection, capability-check и healthcheck output.
3. Зафиксировать Linux behavior regression, затем добавить native CI/build/test
   matrix для macOS и Windows; ConPTY и native credential stores не эмулировать
   Unix-предположениями.
4. Проверить Unicode/пробелы в paths, отсутствующие shell/ssh/editor/runtime и
   безопасное деградирование каждой optional capability.

Оставшаяся часть этапа: вынести process lifecycle/signals, OpenSSH launch,
credential implementation и container runtime execution за platform interfaces;
реализовать настоящий ConPTY и native credential stores, затем расширить native
CI с core smoke до интерактивных acceptance tests.

### Этап 2 — Sources/Groups/Views navigation

**Groups MVP завершён 2026-08-30; Views и расширенный source status продолжаются.**
Левая панель теперь визуально разделяет `SOURCES` и `GROUPS`. Private groups,
созданные из TUI, хранятся по одной в строгих mode-0600 fragments под
`groups.d`; `n` создаёт, host `m` или Enter → `Manage group membership` меняет
membership по stable `source:alias`, `R` переименовывает, `D` удаляет, `e`
открывает fragment в выбранном editor.
Main-config groups остаются read-only для CRUD. Storage/model и настоящий PTY
проход с create → two memberships → rename → delete сохраняют screenshots.

1. Перестроить левую панель на секции `SOURCES`, `GROUPS`, `VIEWS`, сохраняя
   `HOSTS` главным и выбранный host/Preview при безопасном переключении.
2. Добавить keyboard CRUD membership: create/rename/delete group, add/remove
   host, move между private groups как `add target + remove source group`.
3. Показывать source kind/origin/path-or-URL, group membership и состояния
   loaded/loading/stale/partial/error без credential values.
4. Добавить model + real PTY traversal/screenshots для duplicate aliases,
   missing members, empty groups, unavailable source и group command plan.

Оставшаяся часть этапа: вычисляемые `VIEWS`, отдельные source state/origin rows,
multi-select membership и явное отображение missing members/duplicate aliases.

### Этап 3 — terminal tabs MVP

**Завершён 2026-08-30 для Unix PTY; native ConPTY остаётся в cross-platform
foundation.** Постоянная Fleet tab и несколько SSH/local/container/workspace/log
tabs имеют независимые PTY/VT, resize, running/exited/error state, bounded tail,
основной прямой выбор `Alt+1…9` с optional `Ctrl+1…9`, переключение
`Ctrl+N/P`, возврат `Ctrl+G`,
немедленное локальное закрытие `Ctrl+D`, автовозврат во Fleet после завершения
активной сессии, terminal-local `q`,
bracketed paste passthrough и подтверждаемое закрытие `Ctrl+]`.
Model tests и настоящий PTY E2E открывают две одновременные SSH tabs, проходят
local/container tabs, сохраняют screenshots и проверяют live-close confirmation.

1. Выделить session manager и постоянную `Fleet` tab; открыть несколько
   независимых local/SSH/container PTY/ConPTY sessions.
2. Реализовать open/switch/list/close/return-to-Fleet, visible active/error/
   disconnected states и подтверждение закрытия живого foreground process.
3. Изолировать input/output/resize и bounded session tail; исключить secrets и
   shell history из title, metadata, logs и restore state.
4. Покрыть параллельные sessions, быстрые переключения, массовый open/close и
   отсутствие goroutine/process/PTY leaks настоящими E2E.

### Этап 4 — remote workspace UX и portability

1. Добавить Preview-режим для bundled `lf`, `nvim`, `dtop` и tools shell;
   terminal-tab режим уже использует те же provenance/lifecycle/cleanup гарантии.
2. Добавить Linux/arm64 и musl bundles; после native foundation спроектировать
   отдельные macOS/Windows companion artifacts без обещания ложной portability.
3. Исследовать cache-by-hash в пределах одного запуска, не меняя default cleanup
   и не оставляя разделяемые исполняемые каталоги на цели.
4. Отдельным spike исследовать container workspace bootstrap через `docker cp`:
   signed artifact, exact target ID, compatibility, confirmation и cleanup.

### Этап 5 — источники и hardening

1. Доделать offline fallback remote cache и TLS SPKI pinning.
2. Добавить native KWallet API и усилить обращение с краткоживущими secret
   buffers.
3. Подготовить общий read-only source API для будущей веб-версии без поддержки
   удалённого исполняемого `ssh_config`.

### Будущий epic — live settings, SSH profiles и session persistence

Эти задачи связаны одной моделью конфигурационных слоёв и должны развиваться
последовательно, без изменения исходных Sources:

```text
system policy/defaults (read-only)
→ user application config + private overlays
→ saved session profile
→ current launch/session overrides (memory only)
```

Приоритет effective settings: current session → saved session → user config →
system/default policy. Preview и healthcheck всегда должны показывать effective
value, origin layer и причину, почему настройка недоступна или переопределена.

1. **Live application settings.** Добавить TUI-меню собственных настроек
   приложения: редактирование validated draft, preview diff, atomic mode-0600
   save с backup, reload без перезапуска там, где это безопасно, и rollback при
   ошибке. Настройки, требующие restart/reconnect, помечаются явно и не
   применяются частично.
2. **Scopes меню.** Разделять `System`, `User`, `Saved session` и `This launch`.
   System policy только читается; User сохраняется в основном TOML/private
   fragments; This launch живёт только в памяти и исчезает после выхода.
3. **Принудительные secure connection profiles.** Ввести именованные режимы
   безопасного соединения, которые могут только усиливать baseline: host-key
   verification никогда не выключается, forwarding/local commands остаются
   запрещены по умолчанию, ослабление algorithms/auth policy требует явного
   объяснения и не допускается remote/restricted source. До реализации нужно
   определить versioned profile schema и capability diagnostics.
4. **Customize connection.** Действие над существующим Host создаёт или
   обновляет private overlay по stable identity `source:alias`, показывает diff
   effective OpenSSH options и не редактирует исходный `~/.ssh/config`, trusted
   custom SSH config или remote inventory. Разрешены только поля строгой схемы;
   `ProxyCommand`, `LocalCommand` и произвольные executable strings не попадают
   в приватный inventory автоматически.
5. **Автоматическая группа `MY`.** При первой приватной кастомизации приложение
   атомарно создаёт private group с display name `MY` (если она отсутствует) и
   добавляет туда stable host identity. Это membership/view, а не копирование
   секрета или перенос Host из Source. Конфликт с read-only group `MY` должен
   завершаться fail-closed и предлагать другое private имя.
6. **Session persistence.** После появления terminal tabs добавить сохранение и
   восстановление session plan: targets, tab order, selected group/view,
   workspace kind и безопасные non-secret preferences. По умолчанию не
   сохраняются terminal scrollback, команды, пароли, decrypted keys, agent
   sockets и живые процессы. Восстановление создаёт новые подключения после
   preview/confirmation, а не обещает checkpoint удалённого процесса.

Порядок реализации epic: сначала configuration layering/live reload, затем
settings UI и secure profiles, после них per-host customization + `MY`; session
persistence строится поверх готового terminal session manager.

### Этап 6 — Web/Hub automation

1. Ввести read-only web inventory и RBAC, затем immutable host sets и только
   после этого безопасный job scheduler для command presets.
2. Реализовать временный one-shot Foliage-compatible deep inventory без
   постоянного root agent и NATS credential на цели.
3. После стабилизации plan/approval/audit добавить первый управляемый lifecycle
   workflow установки — pinned Dozzle с private-by-default networking,
   verification и rollback.

### Реализованный baseline

1. Responsive desktop layout `8 / 68 / 24`, адаптивные длинные CPU%/MEM% полосы
   и PTY screenshot-проверки medium/wide.
2. Полный и Preview SSH-терминалы с восстановлением TUI и session tail.
3. Enter-menu с проходом всех selectable rows в model и настоящем PTY.
4. Checksummed Linux/amd64 + glibc 2.34 workspace для `lf`, `nvim`, `dtop`,
   `bat` и shell; закрытая форма архива, explicit upload, no forwarding и
   cleanup по умолчанию.
5. Реальный запуск bundle на Ubuntu 22.04, disposable Docker sshd и Debian 12,
   включая интерактивные `lf`, Neovim, `dtop` и проверку отсутствия leftovers.
6. Клавиша `c` редактирует основной TOML через pinned `nvim`; `e` по-прежнему
   редактирует отдельный host overlay, не затрагивая исходный source.
7. Tool healthcheck и resolver выбирают SSH Fleet Console-owned binaries раньше
   system `PATH`; editor fallback — `nvim`, `vim`, `nano`.
8. Cross-source groups работают как virtual filters; локальные argv presets
   запускаются только после plan/confirm с bounded concurrency/output.
9. Embedded SSH временно расширяет Preview и восстанавливает configured layout
   после выхода; model и настоящий PTY проверяют обе фазы.
10. Внешние terminal strings проходят sanitisation, а `make regression`
    сохраняет manifest, logs, coverage, binaries и PTY screenshots.
11. Полноэкранный editor получает реальные TTY file descriptors без capture
    pipes; отдельный PTY E2E запускает настоящий Neovim, нажимает Down и
    проверяет смену строки. Healthcheck показывает configured проценты и
    фактические ширины главных/вложенных колонок в terminal cells.
12. Core release является однобинарным: тот же `sshfleet` работает отдельным
    AskPass-процессом, а `healthcheck`/`doctor` диагностирует required и optional
    capabilities; installer E2E проверяет архив без `sshf-askpass`.
13. Каждая сборка содержит branch/commit provenance; `make test-version`
    проверяет human/JSON output, clean/dirty/unknown state, безопасную build
    metadata, strict и monotonic SemVer gate, совпадение commit/source date/hash
    в VERSION manifest, воспроизводимость native archive и arm64 artifact.
    Отдельный contract-test фиксирует verify → protected publish topology
    GitHub Actions. Весь version suite является обязательным шагом полного
    regression, а stable workflow создаёт тег только после regression на
    текущем чистом `main`.
14. Текущий продукт и repository называются SSH Fleet Console /
    `sshfleet-console`; канонический binary/command — `sshfleet`, optional
    collision-safe alias — `sf`, переходный legacy-alias — `sshf`. Namespace
    config/data/credentials остаётся `sshfleet`.
15. Trusted `local_config` поддерживает direct localhost и local sshd с
    настраиваемыми shell/port; running Docker/Podman containers появляются
    динамически и имеют отдельные shell/Preview/log actions без SSH inheritance.
    Будущие `sshfleet-web` и `sshfleet-hub` зафиксированы как отдельные
    репозитории с versioned contract integration.
16. README переработан в короткий вход в продукт; все реализованные capabilities,
    UI paths, TOML/CLI, trust boundaries и термины разнесены по каноническим
    guide/config/features/security/glossary. `make test-docs` проверяет наличие
    справочников, локальные ссылки и синхронизацию публичных flags/settings/menu.
17. Cross-platform foundation централизует OS capabilities, portable user paths,
    executable rules и local shell resolution. Global `[terminal]` имеет
    отдельный argv и precedence CLI → TOML → OS auto; Linux regression и
    macOS/Windows native CI builds являются обязательными, а отсутствующие
    ConPTY/native stores показываются как capability gap без скрытого fallback.
18. Remote workspace применяет временный ENV-overlay по типу `$SHELL`, а не по
    имени Linux-дистрибутива: исходные `PATH`/`XDG_CONFIG_HOME` видны
    пользовательскому rc, после чего bundle снова получает приоритет. Matrix
    `bash`/`zsh`/`fish`/POSIX shell и fallback неизвестной оболочки покрыты
    автотестами; Docker SSH E2E запускает команды из интерактивного shell.
19. Исходники SSH Fleet Console лицензированы по Apache-2.0. Core release
    содержит `LICENSE`, `NOTICE`, production dependency inventory и точные
    upstream license texts; `make test-licenses` сверяет их с реально linked Go
    modules. Optional companion workspace остаётся отдельным local-only
    artifact и не получает ложного обещания готовности к redistribution.

## Definition of Done

Изменение завершено, если:

1. Оно поддерживает одну из целей и не нарушает trust boundaries.
2. Пользовательский сценарий понятен без чтения исходного кода.
3. Ошибки видны и завершаются fail-closed.
4. Критический путь покрыт unit/model/integration или PTY E2E тестом.
5. Пройдены релевантные make targets, `go vet`, build и race detector.
6. Обновлены эта заметка, README/example config и security backlog, если контракт
   продукта или безопасности изменился.
7. Каждая изменённая публичная capability отражена в README или каноническом
   guide/config/features/security/glossary; `make test-docs` проходит.
8. Публичные изображения создаются только fixture pipeline, проходят marker/IP
   audit и человеческий visual review; реальные fleet/regression screenshots не
   используются как marketing assets.

## Осознанно не завершено

- Веб-интерфейс.
- Unattended command presets, immutable host sets и multi-host web job scheduler.
- Временный Foliage-compatible full inventory collector.
- Управляемая установка, upgrade и rollback Dozzle.
- Установка агента мониторинга на серверы.
- Offline fallback remote cache и TLS SPKI pinning.
- Native KWallet API без Secret Service compatibility layer.
- Полная locked-memory гарантия для краткоживущих секретных buffers.
- Preview-режим для bundled remote workspace и дополнительные архитектуры кроме
  Linux/amd64.
- Отдельный transport для distroless-контейнеров без предположения о наличии
  shell, `tar`, libc и writable home внутри image.

Эти пункты нельзя реализовывать ценой ослабления текущей модели безопасности.
