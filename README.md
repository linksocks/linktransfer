[![GitHub Stars](https://img.shields.io/github/stars/linksocks/linktransfer?style=flat&logo=github&v=2)](https://github.com/linksocks/linktransfer) [![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/linksocks/linktransfer/ci.yml?logo=github&label=Tests&v=2)](https://github.com/linksocks/linktransfer/actions) ![Go Version](https://img.shields.io/github/go-mod/go-version/linksocks/linktransfer) [![Go Reference](https://pkg.go.dev/badge/github.com/linksocks/linktransfer.svg)](https://pkg.go.dev/github.com/linksocks/linktransfer) [![Go Report Card](https://goreportcard.com/badge/github.com/linksocks/linktransfer)](https://goreportcard.com/report/github.com/linksocks/linktransfer) [![Docker Pulls](https://img.shields.io/docker/pulls/jackzzs/linktransfer)](https://hub.docker.com/r/jackzzs/linktransfer) ![License](https://img.shields.io/github/license/linksocks/linktransfer)

# Linktransfer

Linktransfer is a CLI tool for sending files, folders, or text between two machines using a short code.

[中文文档 / Chinese README](README.cn.md)

## Features

1. Send and receive with a simple command — no server setup required.
2. Nearly unlimited traffic with high speed.
3. Resumable transfers — interrupted uploads and downloads pick up where they left off.
4. Automatic NAT traversal on LAN — seamless local network transfers.
5. Self-hostable relay server via [linksocks.js](https://github.com/linksocks/linksocks.js), deployable on Cloudflare.
6. Written in Go — lightweight, fast, and cross-platform.

## Quick Start

### Send a file

```bash
# Sender side: pick a file to send
lt send ./path/to/file

# The terminal prints a receive command, e.g.:
#   lt recv 2f4e8c1d4a9b7c10

# Receiver side: run the command printed above
lt recv 2f4e8c1d4a9b7c10
```

### Send text

```bash
# Sender side
lt send --text "hello from linktransfer"

# Receiver side
lt recv <code>
```

### Use a self-hosted server

```bash
# Sender side
lt send ./file --url ws://your-server:8765

# Receiver side
lt recv <code> --url ws://your-server:8765
```

## Installation

### Golang Version

```bash
go install github.com/linksocks/linktransfer/cmd/lt@latest
```

Or download pre-built binaries from [releases page](https://github.com/linksocks/linktransfer/releases).

### Docker

```bash
# Send a file
docker run --rm -i jackzzs/linktransfer send ./file

# Receive a file
docker run --rm -i jackzzs/linktransfer recv <code>
```

## Self-Hosting the Server

By default, linktransfer connects to the public linksocks service at `ws://l.zetx.tech`. You can deploy your own relay server for full control over the transfer channel.

[linksocks.js](https://github.com/linksocks/linksocks.js) is a lightweight relay server that can be deployed on Cloudflare Worker.

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/linksocks/linksocks.js)

Once deployed, point both sides to your server with `--url`:

```bash
lt send ./file --url ws://your-server:8765 --token your_secret_token
lt recv <code> --url ws://your-server:8765 --token your_secret_token
```

See the [linksocks documentation](https://github.com/linksocks/linksocks) for more options.


## License

Linktransfer is open source under the MIT license.
