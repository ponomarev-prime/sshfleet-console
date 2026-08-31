# Ручная проверка companion toolchain

Эти сценарии проверяют собранные SSH Fleet Console версии `lf`, `dtop`, Neovim и
`bat`, наши отслеживаемые конфиги, temporary remote workspace и отсутствие
постоянной установки в пользовательскую или удалённую систему.

Для обычного использования activation не нужен: после `make app-ready`
приложение запускается одной командой `sshfleet`. Activation ниже нужен только для
ручной проверки companion tools по отдельности.

## 0. Автоматическая подготовка

```sh
cd /path/to/sshfleet-console
make app-ready
sshfleet --help
source ./tools/activate.fish
type -p sshfleet lf dtop nvim bat batcat
```

Для Bash вместо этого используется `. ./tools/activate.sh` и
`command -v lf dtop nvim bat batcat`.

Ожидается:

- сборка заканчивается `toolchain verify: ok`;
- `sshfleet` разрешается через `~/.local/bin/sshfleet`, а companion-команды после
  developer activation — в `$PWD/tools/bin/`;
- `git status --short` остаётся пустым;
- `.toolchain/` существует, но `git check-ignore .toolchain/bin/lf` завершается
  успешно.

Пользовательский launcher не зависит от текущего shell или каталога:

```sh
sh -c 'cd /tmp && sshfleet -version'
bash -c 'cd /tmp && sshfleet -version'
zsh -c 'cd /tmp && sshfleet -version'
fish -c 'cd /tmp && sshfleet -version'
```

Во всех доступных shell ожидается строка `sshfleet dev` (либо версия релизной
сборки).

## 1. Версии и source locks

```sh
make toolchain-smoke
git submodule status
```

Ожидаются `lf r42`, `dtop 0.9.0`, `NVIM v0.12.5`, `bat 0.26.1` и четыре
ревизии без префикса `-` или `+`. `tools/manifest.toml` должен содержать те же
commit SHA и официальные URL.

## 2. `lf`: layout, preview и Neovim

```sh
tmp_dir=$(mktemp -d)
printf '%s\n' '# sshfleet preview' >"$tmp_dir/readme.md"
mkdir "$tmp_dir/subdir"
lf "$tmp_dir"
```

Проверить руками:

1. Видны три колонки с рамками и соотношением примерно `1:2:4`.
2. Скрытые файлы показываются, нижняя строка содержит размер и время.
3. При выборе `readme.md` справа появляется цветной preview через наш `bat`.
4. Нажатие `e` открывает наш Neovim; видны номера строк и cursor line.
5. `:q` возвращает в `lf`, `q` закрывает `lf`.

Для другого редактора используется явный override:

```sh
SSHFLEET_EDITOR=vim lf "$tmp_dir"
```

## 3. Neovim: изолированный профиль

```sh
nvim --headless \
  '+lua print(vim.o.number, vim.o.relativenumber, vim.o.cursorline)' \
  +qa
```

Ожидается `true true true`. Интерактивно можно выполнить `:scriptnames`: первым
пользовательским файлом должен быть `tools/config/nvim/init.lua`, а не
`~/.config/nvim/init.lua`.

## 4. `bat` и Ubuntu-совместимость

```sh
bat --config-file
bat README.md
batcat README.md
```

Первая команда должна вернуть
`$PWD/tools/config/bat/config`. Обе команды просмотра
дают одинаковую подсветку, номера строк и автоматический pager.

## 5. `dtop`: локальный Docker

```sh
docker info
dtop
```

Проверить руками:

1. Список контейнеров загружается без отдельного `--host`.
2. По умолчанию сортировка идёт по CPU, остановленные контейнеры скрыты.
3. Видны колонки status, name, host, CPU, memory, network, uptime и restarts.
4. `Enter` открывает action menu, `Esc` закрывает его, `q` возвращает в shell.

Если `docker info` не работает, ожидаемая диагностика — недоступный daemon;
launcher не должен пытаться использовать `sudo` или менять Docker context.

## 6. Remote workspace из SSH Fleet Console

```sh
sshfleet
```

1. Выбрать Linux/amd64 + glibc 2.34+ узел и нажать `Enter`.
2. Выбрать единственный пункт `Open SSH Fleet workspace`.
3. В открытом shell проверить команды `lf`, `nvim`, `dtop` и `bat`. В `lf`
   Enter на файле до 50 строк открывает `bat` без pager, на файле от 51 строки —
   `bat` через `less` при его наличии либо built-in pager; `e` открывает bundled
   Neovim. `dtop`
   использует Docker daemon именно удалённого узла.
   Повторить на `bash`, `zsh`, `fish` и `/bin/sh`: даже если пользовательский rc
   полностью заменяет `PATH`, `command -v lf nvim dtop bat` должен указывать на
   текущий `/tmp/sshfleet-workspace.*/bin`.
4. `q`/`:q`/`exit` возвращают в shell/workspace, а `exit` из workspace — в SSH
   Fleet Console. На цели после выхода не должно быть
   `/tmp/sshfleet-workspace.*`, если `workspace_cleanup = true`.
5. Нажать `c`: основной `~/.config/sshfleet/config.toml` должен открыться в
   локальном pinned Neovim с нашим `tools/config/nvim/init.lua`; `:q` возвращает
   в TUI, а новые app/source settings применяются после restart.

Автоматический эквивалент positive-path:

```sh
make test-remote-bundle
make test-workspace-docker
```

Bundle не передаёт SSH keys, passwords, agent socket, локальный Docker socket
или `~/.ssh/config`, не вызывает `sudo`/package manager и fail-closed отклоняет
другую architecture, musl и glibc ниже 2.34.

## 7. Изоляция пользовательских конфигов

До и после запуска инструментов сравнить:

```sh
stat ~/.config/lf/lfrc ~/.config/nvim/init.lua ~/.config/dtop/config.yaml \
  ~/.config/bat/config 2>/dev/null || true
git status --short
```

Ожидается, что пользовательские файлы не создавались и не менялись, а в Git не
появились бинарники, runtime или build-кэш.

## 8. Повторная сборка

```sh
make toolchain-ready
make toolchain-ready
```

Оба запуска должны завершиться успешно. Второй использует существующие Cargo,
CMake и Go результаты; версии и source revisions после него не меняются.
