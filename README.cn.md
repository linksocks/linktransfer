[![GitHub Stars](https://img.shields.io/github/stars/linksocks/linktransfer?style=flat&logo=github)](https://github.com/linksocks/linktransfer) [![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/linksocks/linktransfer/ci.yml?logo=github&label=Tests)](https://github.com/linksocks/linktransfer/actions) ![Go Version](https://img.shields.io/github/go-mod/go-version/linksocks/linktransfer) [![Go Reference](https://pkg.go.dev/badge/github.com/linksocks/linktransfer.svg)](https://pkg.go.dev/github.com/linksocks/linktransfer) [![Go Report Card](https://goreportcard.com/badge/github.com/linksocks/linktransfer)](https://goreportcard.com/report/github.com/linksocks/linktransfer) [![Docker Pulls](https://img.shields.io/docker/pulls/jackzzs/linktransfer)](https://hub.docker.com/r/jackzzs/linktransfer) ![License](https://img.shields.io/github/license/linksocks/linktransfer)

# Linktransfer

Linktransfer 是一个通过短码在两台机器之间传输文件、文件夹或文本的命令行工具。

[English / 英文文档](README.md)

## 功能

1. 两端各一条命令即可完成收发，无需预配服务器。
2. 近乎无限流量，高速传输。
3. 断点续传，传输中断后自动恢复。
4. 内网自动打洞，局域网传输无需额外配置。
5. 可通过 [linksocks.js](https://github.com/linksocks/linksocks.js) 自部署中转服务器，支持部署到 Cloudflare。
6. 使用 Go 编写，轻量高效，跨平台支持。

## 快速开始

### 发送文件

```bash
# 发送端：选择要发送的文件
lt send ./path/to/file

# 终端会输出接收命令，例如：
#   lt recv 2f4e8c1d4a9b7c10

# 接收端：运行上面输出的命令
lt recv 2f4e8c1d4a9b7c10
```

### 发送文本

```bash
# 发送端
lt send --text "hello from linktransfer"

# 接收端
lt recv <code>
```

### 使用自部署服务器

```bash
# 发送端
lt send ./file --url ws://your-server:8765

# 接收端
lt recv <code> --url ws://your-server:8765
```

## 安装

### Golang 版本

```bash
go install github.com/linksocks/linktransfer/cmd/lt@latest
```

或从 [Releases 页面](https://github.com/linksocks/linktransfer/releases) 下载预编译二进制文件。

### Docker

```bash
# 发送文件
docker run --rm -i jackzzs/linktransfer send ./file

# 接收文件
docker run --rm -i jackzzs/linktransfer recv <code>
```

## 自部署服务器

默认情况下，linktransfer 连接公共 linksocks 服务（`ws://l.zetx.tech`）。如需完全掌控传输通道，可自行部署中转服务器。

[linksocks.js](https://github.com/linksocks/linksocks.js) 是轻量中继服务，可部署到 Cloudflare Worker。

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/linksocks/linksocks.js)

部署完成后，两端通过 `--url` 指定服务器即可：

```bash
lt send ./file --url ws://your-server:8765
lt recv <code> --url ws://your-server:8765
```

更多选项请参阅 [linksocks 文档](https://github.com/linksocks/linksocks)。


## 许可证

Linktransfer 基于 MIT 许可证开源。

