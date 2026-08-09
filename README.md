# Port Forwarder

A lightweight TCP port-forwarding daemon with an embedded web admin UI. It
lets you manage multiple forwarding rules in the browser, without writing any
config file.

## Features

- Pure TCP forwarding, no encryption or auth (yet)
- Embedded web admin UI, zero external assets
- REST API for rule management
- Single static binary, cross-compiled for Linux and Windows

## Build

Requires Go 1.25+.

```sh
make all        # builds bin/port-forwarder-{linux,windows}-amd64(.exe)
make test       # runs the test suite
```

## Usage

```sh
./bin/port-forwarder-linux-amd64 local -web :28774
```

Then open <http://localhost:28774/> in a browser and add rules:

| Field  | Meaning                                  | Example        |
| ------ | ---------------------------------------- | -------------- |
| name   | A label for the rule                     | `http-dev`     |
| listen | Local address to listen on               | `:8080`        |
| target | Address to forward every connection to   | `127.0.0.1:3000` |

The default web UI port `28774` is derived from the CLI name: `pf` in ASCII
is `0x70 0x66`, which concatenated is `0x7066`, or `28774` in decimal.

## Reverse forwarding over SSH

To expose a port of a machine behind NAT, without installing anything on the
public host, run the daemon (`pf`) and manage everything from the web UI. A
remote rule behaves like `ssh -R`: `pf` logs into a plain `sshd`, and the
tunnel is carried inside ordinary SSH sessions.

Credentials are stored separately from rules so one SSH login can back many
rules. In the web UI:

1. In the **认证信息** (credentials) panel, add a credential: name, host,
   user, and either a password or a private key path. Press **测试** to verify
   `pf` can actually log in.
2. Switch the rule form to **远程转发 (ssh -R)**, pick the credential, and add
   a rule:

| Field  | Meaning                           | Example             |
| ------ | --------------------------------- | ------------------- |
| listen | Public port to open on the server | `:7788`             |
| target | Private address behind the NAT    | `192.168.1.2:7777`  |

Connections to `vps.example.com:7788` then reach `192.168.1.2:7777` through
the SSH tunnel. Deleting a credential also removes every rule that used it.

Credentials persist in `~/.config/pf/credentials.json` (0600). Rules are
in-memory only.

### Prerequisites on the public host

The public host only runs a stock `sshd`. Two settings matter:

- **GatewayPorts** — by default `ssh -R` binds the remote port to
  `127.0.0.1`, which is not reachable from the Internet. To publish it on the
  public interface, set `GatewayPorts yes` in `/etc/ssh/sshd_config` and
  restart sshd. (Even when it is `no`, the reverse-forward still works for
  localhost connections.)
- Credentials: a password credential uses the password; a key credential
  tries the ssh-agent first, then the configured key.

## REST API

| Method   | Path                     | Description        |
| -------- | ------------------------ | ------------------ |
| GET      | `/api/rules`             | List all rules     |
| POST     | `/api/rules`             | Create a rule      |
| DELETE   | `/api/rules/{id}`        | Delete a rule      |
| POST     | `/api/rules/{id}/restart`| Restart all rules  |
| GET      | `/api/health`            | Health check       |
| GET      | `/api/credentials`       | List credentials   |
| POST     | `/api/credentials`       | Create a credential |
| POST     | `/api/credentials/{id}/probe` | Test an SSH login |
| DELETE   | `/api/credentials/{id}`  | Delete a credential |

Create a rule:

```sh
curl -X POST http://localhost:28774/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"name":"http-dev","listen":":8080","target":"127.0.0.1:3000"}'
```

## License

MIT
