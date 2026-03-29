package linktransfer

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/linksocks/linksocks/linksocks"
	"github.com/rs/zerolog"
	"github.com/schollz/croc/v10/src/comm"
	"github.com/schollz/croc/v10/src/croc"
	"github.com/schollz/croc/v10/src/tcp"
	log "github.com/schollz/logger"
	"github.com/spf13/cobra"
)

const defaultWSURL = "ws://l.zetx.tech"

const (
	relayPortMin   = 20000
	relayPortMax   = 60000
	relayPortCount = 5
)

type tunnelOptions struct {
	URL   string
	Token string
	Debug bool
}

func tokenFromCode(code string) string {
	h := sha256.Sum256([]byte("lt:" + code))
	return hex.EncodeToString(h[:16])
}

func relayBasePortFromCode(code string) int {
	h := sha256.Sum256([]byte("lt:relay:" + code))
	v := binary.BigEndian.Uint32(h[:4])

	slots := (relayPortMax - relayPortMin + 1 - relayPortCount) / relayPortCount
	if slots < 1 {
		return relayPortMin
	}
	slot := int(v % uint32(slots))
	return relayPortMin + slot*relayPortCount
}

func relayPortsFromBase(basePort, numPorts int) []string {
	ports := make([]string, 0, numPorts)
	for i := 0; i < numPorts; i++ {
		ports = append(ports, strconv.Itoa(basePort+i))
	}
	return ports
}

func relayPortsFromCode(code string) []string {
	return relayPortsFromBase(relayBasePortFromCode(code), relayPortCount)
}

func newLinksocksLogger(debug bool) zerolog.Logger {
	output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	if !debug {
		output.PartsExclude = []string{
			zerolog.TimestampFieldName,
		}
	}
	logger := zerolog.New(output).With().Timestamp().Logger()
	if debug {
		logger = logger.Level(zerolog.DebugLevel)
	} else {
		logger = logger.Level(zerolog.Disabled)
	}
	return logger
}

type tunnelRuntime struct {
	close func()
}

func startSenderTunnel(ctx context.Context, t tunnelOptions) (*tunnelRuntime, error) {
	logger := newLinksocksLogger(t.Debug)

	opt := linksocks.DefaultClientOption().
		WithWSURL(t.URL).
		WithReverse(true).
		WithSocksHost("127.0.0.1").
		WithSocksPort(0).
		WithSocksWaitServer(true).
		WithReconnect(true).
		WithLogger(logger)

	client := linksocks.NewLinkSocksClient("", opt)
	if err := client.WaitReady(ctx, 0); err != nil {
		client.Close()
		return nil, fmt.Errorf("tunnel failed: %w", err)
	}

	if _, err := client.AddConnector(t.Token); err != nil {
		client.Close()
		return nil, fmt.Errorf("register connector token: %w", err)
	}

	return &tunnelRuntime{close: func() {
		_ = client.RemoveConnector(t.Token)
		client.Close()
	}}, nil
}

func startReceiverTunnel(ctx context.Context, t tunnelOptions) (*tunnelRuntime, error) {
	socksHost := "127.0.0.1"
	socksBasePort := 18700
	socksMaxTries := 5

	logger := newLinksocksLogger(t.Debug)

	var client *linksocks.LinkSocksClient
	var socksPort int

	for i := 0; i < socksMaxTries; i++ {
		port := socksBasePort + i
		ln, err := net.Listen("tcp", net.JoinHostPort(socksHost, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = ln.Close()

		opt := linksocks.DefaultClientOption().
			WithWSURL(t.URL).
			WithReverse(false).
			WithSocksHost(socksHost).
			WithSocksPort(port).
			WithSocksWaitServer(true).
			WithReconnect(true).
			WithLogger(logger)

		client = linksocks.NewLinkSocksClient(t.Token, opt)
		if err := client.WaitReady(ctx, 0); err != nil {
			client.Close()
			continue
		}
		socksPort = port
		break
	}

	if client == nil {
		return nil, fmt.Errorf("no available SOCKS5 port in range %d-%d", socksBasePort, socksBasePort+socksMaxTries-1)
	}

	comm.Socks5Proxy = fmt.Sprintf("%s:%d", socksHost, socksPort)
	return &tunnelRuntime{close: client.Close}, nil
}

func setupLocalRelay(basePort, numPorts int, password string) ([]string, error) {
	ports := relayPortsFromBase(basePort, numPorts)

	for _, p := range ports {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", p))
		if err != nil {
			return nil, fmt.Errorf("port %s is not available on 127.0.0.1 (need %d consecutive ports starting at %d): %w", p, numPorts, basePort, err)
		}
		_ = ln.Close()
	}

	debugLevel := "warn"

	tcpPorts := strings.Join(ports[1:], ",")
	for i := 1; i < len(ports); i++ {
		go func(p string) {
			_ = tcp.Run(debugLevel, "127.0.0.1", p, password)
		}(ports[i])
	}
	go func() {
		_ = tcp.Run(debugLevel, "127.0.0.1", ports[0], password, tcpPorts)
	}()

	// Wait for relay to start accepting connections.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := tcp.PingServer(net.JoinHostPort("127.0.0.1", ports[0])); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("local relay did not become ready on 127.0.0.1:%s", ports[0])
		}
		time.Sleep(50 * time.Millisecond)
	}

	return ports, nil
}

func applyCommonCrocOptions(ops *croc.Options) {
	// croc uses github.com/schollz/logger (a global logger). The upstream croc
	// package sets it to debug in init(), which causes noisy debug logs before
	// options are applied. Force warn level for clean CLI output.
	log.SetLevel("warn")
	ops.Debug = false
	ops.DisableLocal = true
	ops.OnlyLocal = false
	ops.RelayAddress = "127.0.0.1"
	ops.RelayAddress6 = ""
	ops.Curve = "p256"
	ops.HashAlgorithm = "xxhash"
}

func addTunnelFlags(cmd *cobra.Command, t *tunnelOptions) {
	cmd.Flags().StringVarP(&t.URL, "url", "u", defaultWSURL, "linksocks server URL")
	cmd.Flags().StringVarP(&t.Token, "token", "t", "", "linksocks token (derived from code if omitted)")
	cmd.Flags().BoolVar(&t.Debug, "debug", false, "Enable debug logs")
}
