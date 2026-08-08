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
