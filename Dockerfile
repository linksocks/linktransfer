# Build stage
ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git

ARG GOPROXY=https://proxy.golang.org|direct

COPY go.mod go.sum ./
RUN GOPROXY="$GOPROXY" go mod download

COPY . .

ENV GOFLAGS=-buildvcs=false
RUN CGO_ENABLED=0 go build -o lt ./cmd/lt

# Final stage
FROM alpine:3.19

WORKDIR /app

COPY --from=builder /src/lt /app/lt

RUN adduser -D -H -h /app linktransfer && chown -R linktransfer:linktransfer /app
USER linktransfer

ENTRYPOINT ["/app/lt"]
