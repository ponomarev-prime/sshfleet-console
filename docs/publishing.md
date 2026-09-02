# Публикация SSH Fleet Console

Этот документ фиксирует действующую public boundary, release checklist и
правила публикации новых изменений. Он не заменяет юридическую консультацию.
Private development history, обычные regression artifacts и реальные fleet
screenshots по-прежнему нельзя отправлять в публичный GitHub.

## Текущий вердикт

На 2026-09-02 проект опубликован как
[`ponomarev-prime/sshfleet-console`](https://github.com/ponomarev-prime/sshfleet-console)
из отдельной clean history. `dev` — default integration branch; `main` и tags
`v*` защищены active rulesets. Environment `release` ограничен protected
branch и требует ручного reviewer. Private vulnerability reporting и CodeQL
включены. Первый проверенный release `v0.1.0` опубликован workflow 2026-08-31
вместе с Linux amd64/arm64 archives, checksums и full regression evidence.

Эта готовность не отменяет границу данных:

1. Private archive commits всё ещё содержат локальные identifiers; нельзя
   делать `push --mirror`, `git push --tags` или подменять public history.
2. Обычные `.artifacts/regression-*` остаются внутренним evidence: SVG и логи
   могут содержать абсолютные paths и пользовательский environment.
3. В public tree попадают только privacy-reviewed fixture screenshots и
   source changes, прошедшие `make regression` и `make audit-public`.

## Рекомендуемая лицензия

Владелец подтвердил **Apache License 2.0** 2026-08-31:

- permissive use, modification and redistribution;
- явный patent grant от contributors;
- patent-termination protection;
- сохранение copyright/license notices и обозначение изменённых файлов;
- подходит для будущих коммерческих Console/Web/Hub интеграций без требования
  открывать производный код.

Корень содержит канонический `LICENSE`, project `NOTICE`, production dependency
inventory `THIRD_PARTY_NOTICES.md` и точные upstream texts в
`third_party/licenses/`. Они входят в amd64/arm64 core archives.
`make test-licenses` строит реальный dependency graph `./cmd/sshf`, сравнивает
версии, пути, SHA-256 и сохранённые тексты и входит в `make check-core` и
`make regression`.

Optional `lf`/`dtop`/Neovim/`bat` и скопированные host runtime libraries не
входят в официальный core release. Локально создаваемый remote workspace не
объявляется redistributable artifact; его отдельные обязательства зафиксированы
в `THIRD_PARTY_NOTICES.md`.

## Чистая публичная история

Private `.git` остаётся development archive. Первый public push уже был сделан
из проверенного snapshot в отдельном staging repository, без старых commits и
локальных archive tags. Процедура ниже сохраняется как воспроизводимый audit
этой границы и recovery recipe. Она сохраняет точное дерево исходного commit,
включая gitlink pins optional toolchain:

```sh
# Выполнять только после добавления LICENSE/NOTICE и зелёного gate.
cd ~/my_code/sshfleet-console
make regression
make audit-public
make test-public-snapshot
make public-snapshot
cd ../sshfleet-console-public
git log --oneline --all
git ls-tree HEAD tools/src/lf tools/src/dtop tools/src/nvim tools/src/bat
```

По умолчанию root commit использует public identity
`ponomarev-prime <ponomarev-prime@users.noreply.github.com>`. Её можно заменить
через `SSHF_PUBLIC_GIT_NAME` и `SSHF_PUBLIC_GIT_EMAIL`. Скрипт отказывается
работать с dirty source, существующим destination или несовпадающим `dev`/HEAD,
восстанавливает gitlinks после `git archive`, требует идентичный tree SHA,
запускает public audit и не создаёт remote/tags. Если нужен подписанный initial
commit, подписать проверенный root перед первым push и снова выполнить audit.

Перед commit вручную проверить автора, все files и submodule pins. Нельзя
копировать `.git`, `.artifacts`, `.tmp`, private inventories, SSH configs,
known_hosts или credentials. Публичные `main` и `dev` в этот момент могут
начинаться с одного clean snapshot; дальнейшие изменения идут через PR.

Локальные milestone tags:

- `archive/pre-tabs` → `b74656c`, последний private snapshot до terminal tabs;
- `archive/tabs` → проверенный текущий snapshot с terminal tabs.

Это private navigation markers, не SemVer и не GitHub Releases. Их нельзя
push-ить в public remote. Первый публичный stable tag остаётся `v0.1.0` и
создаётся только Release workflow из защищённой `main`.

## Создание репозитория в GitHub

1. Открыть `https://github.com/new`, выбрать владельца и имя
   `sshfleet-console`, visibility **Public**.
2. Не добавлять README, `.gitignore` или license через web form: подготовленный
   staging snapshot уже содержит файлы, а web initialization создаст лишнюю
   несвязанную историю.
3. Добавить новый URL как `origin` только в clean public staging repository,
   не в private archive, затем push `main` и `dev` без `--mirror` и `--tags`.
4. Установить `dev` default branch согласно текущей integration policy;
   `main` остаётся release-only.

## Настройки GitHub

### Rulesets

- `main`: запрет force-push/deletion, только PR, linear history, conversation
  resolution, stale approvals dismissed, обязательный `CI / core`, без прямых
  push/bypass.
- `dev`: запрет force-push/deletion, обязательный CI; изменения также через PR,
  когда появляется второй maintainer.
- tags `v*`: запрет update/deletion и ручного создания; разрешить создание
  только проверенному release workflow. Проверить правило на disposable tag до
  первого релиза.
- Включить signed commits, когда все maintainers настроили подпись.

### Actions и release environment

- Settings → Actions → General: default `GITHUB_TOKEN` = read-only contents and
  packages; запретить Actions создавать/approve pull requests.
- Разрешить только необходимые pinned actions. Workflows уже закрепляют actions
  полными commit SHA.
- Создать Environment `release`: deployment branch только `main`, required
  reviewer, prevent self-review и no admin bypass.
- Prevent self-review требует второго доверенного maintainer. До его появления
  автоматическую публикацию stable release не запускать.
- Включить immutable releases; workflow сначала создаёт draft, загружает все
  assets и только затем публикует его.

### Security и collaboration

- Включить Dependabot alerts/security updates, dependency graph, secret
  scanning, push protection и CodeQL/code scanning.
- Включить Private vulnerability reporting и notifications; `SECURITY.md`
  остаётся публичной policy, но reports идут приватно.
- Issues включить после templates для bug/feature; security issue template
  должен отправлять в private vulnerability reporting.
- Wiki и Projects можно оставить выключенными до появления процесса поддержки;
  автоматически удалять head branches после merge.

## Непалевные скриншоты

Нельзя публиковать ручной снимок реального fleet или файлы из обычного
`.artifacts/regression-*`. Использовать только fixture pipeline:

```sh
make test-public-screenshots
```

Он запускает отдельные PTY E2E с `*.example`, documentation-only addresses и
искусственными hosts/groups, заменяет home path на `/home/demo`, отклоняет
private RFC1918 addresses, затем складывает SVG/PNG в
`.artifacts/public-screenshots/`. Рабочие имена остаются предметом обязательной
человеческой проверки ниже.

В public tree вручную просмотрены и скопированы только fixture-файлы из
`docs/assets/screenshots/`: Fleet overview, Actions menu, terminal tabs, groups
и вычисляемый low-memory View. README ссылается только на эти tracked copies;
обычные regression artifacts и остальные generated screenshots не входят в Git.

Перед публикацией всё равно открыть каждый кадр и проверить:

- username, hostname, prompt, home/repository path;
- IP, ports, aliases, groups, process/container names;
- source names, key filenames, credential bindings и session tail;
- title bar терминала, tabs desktop environment и clipboard notifications.

Лучше публиковать SVG/PNG, созданный pipeline, без рамки desktop terminal. Если
нужна фотография настоящего desktop, использовать отдельного demo-user, чистый
HOME, fixtures и documentation networks `192.0.2.0/24`, `198.51.100.0/24` или
`203.0.113.0/24`.

## Go/no-go каждого публичного изменения

Публикация разрешена только когда одновременно:

1. LICENSE/NOTICE/third-party notices присутствуют и `make test-licenses`
   проходит;
2. новый clean public history не содержит private identifiers и проходит
   `gitleaks`;
3. `make regression`, `make audit-public` и public screenshots зелёные;
4. GitHub rulesets, Actions, environment, immutable releases и security
   reporting проверены;
5. public README screenshots просмотрены человеком;
6. новый `vX.Y.Z` создаёт workflow на точном current `origin/main`, а не
   локальная команда.
