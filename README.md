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
./bin/port-forwarder-linux-amd64 -web :28774
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
public host, use the `ssh` mode. It behaves like `ssh -R`: `pf` logs into a
plain `sshd`, and the tunnel is carried inside ordinary SSH sessions.

```sh
# on the NAT-ed device that you control
./bin/port-forwarder-linux-amd64 ssh -host vps.example.com:22

# or with a password, skipping the ssh-agent:
./bin/port-forwarder-linux-amd64 ssh -host vps.example.com -pass 'secret'
```

Then in the web admin UI, switch the form to "远程转发 (ssh -R)" and add a
rule such as:

| Field  | Meaning                           | Example             |
| ------ | --------------------------------- | ------------------- |
| listen | Public port to open on the server | `:7788`             |
| target | Private address behind the NAT    | `192.168.1.2:7777`  |

Connections to `vpn.example.com:7788` then reach `192.168.1.2:7777` through
the SSH tunnel.

### Prerequisites on the public host

The public host only runs a stock `sshd`. Two settings matter:

- **GatewayPorts** — by default `ssh -R` binds the remote port to
  `127.0.0.1`, which is not reachable from the Internet. To publish it on the
  public interface, set `GatewayPorts yes` in `/etc/ssh/sshd_config` and
  restart sshd. (Even when it is `no`, the reverse-forward still works for
  localhost connections.)
- Credentials: authentication uses the ssh-agent first, then `-key`, then
  `-pass`.

### Flags

| Flag        | Meaning                          |
| ----------- | -------------------------------- |
| `-host`     | sshd address, e.g. `vps.example.com:22` |
| `-user`     | SSH login user                   |
| `-key`      | Private key path                 |
| `-passphrase` | Passphrase for an encrypted key |
| `-pass`     | Login password (disables agent auth) |

## REST API

| Method   | Path                     | Description        |
| -------- | ------------------------ | ------------------ |
| GET      | `/api/rules`             | List all rules     |
| POST     | `/api/rules`             | Create a rule      |
| DELETE   | `/api/rules/{id}`        | Delete a rule      |
| POST     | `/api/rules/{id}/restart`| Restart all rules  |
| GET      | `/api/health`            | Health check       |

Create a rule:

```sh
curl -X POST http://localhost:28774/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"name":"http-dev","listen":":8080","target":"127.0.0.1:3000"}'
```

## License

MIT
