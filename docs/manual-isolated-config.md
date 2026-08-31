# Manual isolated configuration path

This walkthrough proves that SSH Fleet Console can run from explicit files without
reading aliases from `~/.ssh/config`. It uses documentation-only addresses and
disables probes, so it does not contact any host.

## 1. Build and create an isolated workspace

```sh
cd ~/my_code/sshfleet-console
make build

MANUAL_ROOT="$(mktemp -d)"
mkdir -p "$MANUAL_ROOT/sources.d" "$MANUAL_ROOT/hosts.d"
printf 'manual workspace: %s\n' "$MANUAL_ROOT"
```

## 2. Write the single application configuration

```sh
cat >"$MANUAL_ROOT/app.toml" <<EOF
version = 1

[app]
refresh_interval = "30s"
connect_timeout = "4s"
max_concurrent = 4
ssh_binary = "ssh"
probe_enabled = false
load_user_ssh_config = false
sources_dir = "$MANUAL_ROOT/sources.d"
overrides_dir = "$MANUAL_ROOT/hosts.d"
source_state_dir = "$MANUAL_ROOT/source-state"
editor = "vi"

[app.ui]
sources_width_percent = 10
preview_width_percent = 24

[[credentials]]
name = "manual-password"
type = "password"
provider = "secret-service"
key = "sshfleet/manual/password-nodes"

[[host_rules]]
source = "manual"
match = "password-node-*"
credential = "manual-password"
EOF
chmod 600 "$MANUAL_ROOT/app.toml"
```

`[app]` contains application behavior. It does not contain source data or
secret values. CLI flags override these settings for one launch.

## 3. Add an explicit trusted OpenSSH source

```sh
cat >"$MANUAL_ROOT/ssh_config" <<'EOF'
Host manual-key
    HostName 192.0.2.10
    User operator
    Port 22
    IdentityFile ~/.ssh/id_ed25519
    IdentitiesOnly yes

Host password-node-*
    User root
    PreferredAuthentications password

Host password-node-1
    HostName 192.0.2.20
    Port 22
EOF
chmod 600 "$MANUAL_ROOT/ssh_config"
```

This file contains aliases and paths to keys, not private-key bytes. The literal
`Host password-node-1` makes that host enumerable; the wildcard
`Host password-node-*` only adds
OpenSSH defaults to matching literal aliases.

Confirm exactly which sources and aliases will load:

```sh
./bin/sshfleet \
  --config "$MANUAL_ROOT/app.toml" \
  --no-user-ssh-config \
  --ssh-config "manual=$MANUAL_ROOT/ssh_config" \
  --list
```

Expected source/hosts: `manual`, `manual-key`, and `password-node-1`. There must be no
`user` source and none of the aliases from `~/.ssh/config`.

Open the TUI without probes:

```sh
./bin/sshfleet \
  --config "$MANUAL_ROOT/app.toml" \
  --no-user-ssh-config \
  --ssh-config "manual=$MANUAL_ROOT/ssh_config"
```

In Preview, check `source: manual` and
`config: $MANUAL_ROOT/ssh_config`. Press `q` to exit.

## 4. Test the restricted SSH Fleet Console inventory source

```sh
cat >"$MANUAL_ROOT/inventory.toml" <<'EOF'
version = 1

[[groups]]
name = "stands"
match = "lab-*"
tags = ["manual", "stand"]

[[hosts]]
alias = "lab-01"
name = "Manual restricted host"
hostname = "192.0.2.30"
user = "operator"
port = 2222
EOF
chmod 600 "$MANUAL_ROOT/inventory.toml"

./bin/sshfleet \
  --config "$MANUAL_ROOT/app.toml" \
  --no-user-ssh-config \
  --inventory "restricted=$MANUAL_ROOT/inventory.toml" \
  --list
```

The restricted source accepts only hosts, groups, tags, routing data, probe
policy, and credential names. It rejects passwords and executable OpenSSH
directives. Connections from it use `-F /dev/null`, so wildcard directives from
the user's default OpenSSH configuration cannot leak into this source.

## 5. Bind a real password later

Only do this when `password-node-*` points to a real test environment and the desktop
Secret Service/KWallet is available:

```sh
./bin/sshfleet credential set manual-password --config "$MANUAL_ROOT/app.toml"
```

The command reads the password twice with terminal echo disabled and stores it
under `sshfleet/manual/password-nodes` in Secret Service. TOML retains only the reference.
For a real probe, change `probe_enabled` to `true`; OpenSSH receives the password
through a separate AskPass process of the same `sshfleet` binary, never argv or
logs. A legacy `sshf-askpass` helper is optional compatibility, not a runtime
requirement.

## 6. Optional persistent source wizard

Instead of the repeatable launch flag, persist the trusted source inside the
isolated `sources.d`:

```sh
./bin/sshfleet source add \
  --type openssh \
  --name manual \
  --path "$MANUAL_ROOT/ssh_config" \
  --config "$MANUAL_ROOT/app.toml"
```

Then omit `--ssh-config` on later launches. The generated fragment is mode
`0600`; the source OpenSSH file remains unchanged.

## 7. Pack and load an encrypted shareable inventory

This step requires `age`, `age-keygen`, and OpenSSH `ssh-keygen`:

```sh
age-keygen -o "$MANUAL_ROOT/age-identity.txt"
chmod 600 "$MANUAL_ROOT/age-identity.txt"
AGE_RECIPIENT="$(age-keygen -y "$MANUAL_ROOT/age-identity.txt")"

ssh-keygen -q -t ed25519 -N '' -f "$MANUAL_ROOT/inventory-signing"
printf 'manual-sealed %s\n' "$(cat "$MANUAL_ROOT/inventory-signing.pub")" \
  >"$MANUAL_ROOT/allowed_signers"
chmod 600 "$MANUAL_ROOT/allowed_signers"

./bin/sshfleet source pack \
  --source-id manual-sealed \
  --revision 1 \
  --inventory "$MANUAL_ROOT/inventory.toml" \
  --output "$MANUAL_ROOT/manual-sealed.bundle" \
  --recipient "$AGE_RECIPIENT" \
  --signing-key "$MANUAL_ROOT/inventory-signing" \
  --expires 24h

./bin/sshfleet source add \
  --type encrypted \
  --name manual-sealed \
  --path "$MANUAL_ROOT/manual-sealed.bundle" \
  --signing-key "$MANUAL_ROOT/allowed_signers" \
  --age-identity-ref "age-plugin:$MANUAL_ROOT/age-identity.txt" \
  --config "$MANUAL_ROOT/app.toml"

./bin/sshfleet --config "$MANUAL_ROOT/app.toml" --no-user-ssh-config --list
```

The bundle directory contains only `manifest.toml`, `manifest.sig`, and
`inventory.toml.age`. In production, prefer an age identity retrieved from
Secret Service rather than the test-only private identity file.

Remote sources use the same three files behind an HTTPS directory URL. Their
bearer token and age identity are separate Secret Service references; remote
OpenSSH configuration is never accepted.
