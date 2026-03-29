# Linktransfer

Linktransfer is a small CLI for sending files, folders, or text from one machine to another with a short code.

By default, it connects to our public [linksocks](https://github.com/linksocks/linksocks) service at `ws://l.zetx.tech`, deployed on top of Cloudflare, so it works out of the box without requiring you to deploy your own tunnel server.

The transfer path is built on top of [linksocks](https://github.com/linksocks/linksocks) and [croc](https://github.com/schollz/croc), but the CLI stays intentionally small: `lt send` on one side, `lt recv` on the other.

## Quick Start

Send from the first machine:

```bash
lt send ./path/to/file
```

linktransfer prints a receive command with a generated code, for example:

```bash
lt recv 2f4e8c1d4a9b7c10
```

Run that on the second machine:

```bash
lt recv 2f4e8c1d4a9b7c10
```

If you do not pass `--url`, both sides use the default public linksocks service at `ws://l.zetx.tech`.

Receive into a specific directory:

```bash
lt recv 2f4e8c1d4a9b7c10 --out ./downloads
```

Send text instead of a file:

```bash
lt send --text "hello from linktransfer"
```

## How It Works

linktransfer combines two components:

1. A [linksocks](https://github.com/linksocks/linksocks) tunnel is used as the control path between the two peers.
2. A local [croc](https://github.com/schollz/croc) relay is started on the sender side and reached through the tunnel by the receiver.

The shared transfer code is used to derive:

- the croc shared secret;
- the default linksocks token;
- a deterministic local relay port range.

This keeps the user-facing workflow simple while preserving compatibility with the underlying transport pieces.

## Requirements

- Go 1.24 or newer to build from source.
- Network access to the configured linksocks server. If `--url` is not provided, the default target is our Cloudflare-backed public server at `ws://l.zetx.tech`.
- Two machines that can both run the `lt` binary.

## Build

Build the short command name used by the CLI help:

```bash
go build -o lt ./cmd/lt
```

Run directly from source:

```bash
go run ./cmd/linktransfer --help
```

## Common Usage

Send multiple paths:

```bash
lt send ./file-a ./dir-b
```

Choose your own code:

```bash
lt send ./archive.tar.gz --code release-2026
```

Use a custom linksocks server on both sides:

```bash
lt send ./file --url ws://example.com:8765
lt recv release-2026 --url ws://example.com:8765
```

Provide a custom token explicitly instead of deriving it from the code:

```bash
lt send ./file --code release-2026 --token custom-token
lt recv release-2026 --token custom-token
```

Enable debug logs for tunnel troubleshooting:

```bash
lt send ./file --debug
```

## Command Reference

### `lt send`

```text
Send files or folders

Usage:
  lt send [file-or-dir]... [flags]
```

Flags:

- `-c, --code string`: code phrase. Random if omitted.
- `--text string`: send text instead of files.
- `-u, --url string`: linksocks server URL. Default: `ws://l.zetx.tech`.
- `-t, --token string`: linksocks token. Derived from the code if omitted.
- `--debug`: enable debug logs.

### `lt recv`

```text
Receive files

Usage:
  lt recv [code] [flags]
```

Flags:

- `-c, --code string`: code phrase.
- `--out string`: output folder. Default: current directory.
- `-u, --url string`: linksocks server URL. Default: `ws://l.zetx.tech`.
- `-t, --token string`: linksocks token. Derived from the code if omitted.
- `--debug`: enable debug logs.

If the code is omitted, `lt recv` also checks the `CROC_SECRET` environment variable.

## Development

Run the package tests:

```bash
go test ./internal/linktransfer/...
```

Check the CLI help:

```bash
go run ./cmd/linktransfer --help
go run ./cmd/linktransfer send --help
go run ./cmd/linktransfer recv --help
```

## Repository Layout

- `cmd/linktransfer`: source entrypoint for running from the repository.
- `cmd/lt`: build target for the `lt` binary.
- `internal/linktransfer`: CLI commands and transfer logic.
- `ref/`: upstream or related reference code kept in-tree for comparison.

## Notes

- The default linksocks server URL is `ws://l.zetx.tech`, which is our public linksocks service deployed on Cloudflare.
- The sender starts a local croc relay on `127.0.0.1` using a deterministic block of consecutive ports derived from the transfer code.
- The receiver starts a local SOCKS5 endpoint and reaches the sender through the linksocks tunnel.
- This repository contains additional container and reference assets, but the verified CLI surface documented here is limited to `send` and `recv`.