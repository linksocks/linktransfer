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
	"sync"
	"time"

	"github.com/linksocks/croc/src/comm"
	"github.com/linksocks/croc/src/croc"
	"github.com/linksocks/croc/src/tcp"
	"github.com/linksocks/linksocks/linksocks"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const defaultWSURL = "ws://l.zetx.tech"

const (
	relayPortMin = 20000
	// Keep the range below the ephemeral port pool (Linux: 32768-60999,
	// macOS/Windows: 49152-65535) so outbound connections can never
	// steal a relay port via EADDRINUSE.
	relayPortMax   = 32000
	defaultThreads = 4
)

type tunnelOptions struct {
	URL        string
	Debug      bool
	Threads    int
	DirectMode string
}

func tokenFromCode(code string) string {
	h := sha256.Sum256([]byte("lt:" + code))
	return hex.EncodeToString(h[:16])
}

func relayBasePortFromCode(code string, numPorts int) int {
	h := sha256.Sum256([]byte("lt:relay:" + code))
	v := binary.BigEndian.Uint32(h[:4])

	totalPorts := numPorts + 1 // +1 for control port
	slots := (relayPortMax - relayPortMin + 1 - totalPorts) / totalPorts
	if slots < 1 {
		return relayPortMin
	}
	slot := int(v % uint32(slots))
	return relayPortMin + slot*totalPorts
}

func relayPortsFromBase(basePort, numPorts int) []string {
	ports := make([]string, 0, numPorts+1) // +1 for control port
	for i := 0; i <= numPorts; i++ {
		ports = append(ports, strconv.Itoa(basePort+i))
	}
	return ports
}

func relayPortsFromCode(code string, numPorts int) []string {
	return relayPortsFromBase(relayBasePortFromCode(code, numPorts), numPorts)
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

func applyDirectMode(opt *linksocks.ClientOption, raw string) error {
	if strings.TrimSpace(raw) == "" {
		raw = string(linksocks.DirectModeAuto)
	}
	mode, err := linksocks.ParseDirectMode(raw)
	if err != nil {
		return err
	}
	opt.WithDirectMode(mode)
	return nil
}

type tunnelRuntime struct {
	close        func()
	partnerCount func() int
	disconnected func() <-chan struct{}
}

type localRelayRuntime struct {
	ports  []string
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (r *localRelayRuntime) close() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

func startSenderTunnel(ctx context.Context, t tunnelOptions, token string) (*tunnelRuntime, error) {
	logger := newLinksocksLogger(t.Debug)

	opt := linksocks.DefaultClientOption().
		WithWSURL(t.URL).
		WithReverse(true).
		WithSocksHost("127.0.0.1").
		WithSocksPort(0).
		WithSocksWaitServer(true).
		WithReconnect(true).
		WithLogger(logger)
	if err := applyDirectMode(opt, t.DirectMode); err != nil {
		return nil, err
	}

	client := linksocks.NewLinkSocksClient(os.Getenv("LINKSOCKS_TOKEN"), opt)
	if err := client.WaitReady(ctx, 0); err != nil {
		client.Close()
		return nil, fmt.Errorf("tunnel failed: %w", err)
	}

	if _, err := client.AddConnector(token); err != nil {
		client.Close()
		return nil, fmt.Errorf("register connector token: %w", err)
	}

	return &tunnelRuntime{
		close: func() {
			_ = client.RemoveConnector(token)
			client.Close()
		},
		partnerCount: client.GetPartnersCount,
		disconnected: client.DisconnectedChan,
	}, nil
}

func startReceiverTunnel(ctx context.Context, t tunnelOptions, token string) (*tunnelRuntime, error) {
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
		if err := applyDirectMode(opt, t.DirectMode); err != nil {
			return nil, err
		}

		client = linksocks.NewLinkSocksClient(token, opt)
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
	return &tunnelRuntime{
		close:        client.Close,
		partnerCount: client.GetPartnersCount,
		disconnected: client.DisconnectedChan,
	}, nil
}

func setupLocalRelay(ctx context.Context, basePort, numPorts int, password string) (*localRelayRuntime, error) {
	ports := relayPortsFromBase(basePort, numPorts)

	for _, p := range ports {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", p))
		if err != nil {
			return nil, fmt.Errorf("port %s is not available on 127.0.0.1 (need %d consecutive ports starting at %d): %w", p, numPorts, basePort, err)
		}
		_ = ln.Close()
	}

	debugLevel := "warn"
	relayCtx, cancel := context.WithCancel(ctx)
	runtime := &localRelayRuntime{ports: ports, cancel: cancel}

	tcpPorts := strings.Join(ports[1:], ",")
	for i := 1; i < len(ports); i++ {
		runtime.wg.Add(1)
		go func(p string) {
			defer runtime.wg.Done()
			_ = tcp.RunCtx(relayCtx, debugLevel, "127.0.0.1", p, password)
		}(ports[i])
	}
	runtime.wg.Add(1)
	go func() {
		defer runtime.wg.Done()
		_ = tcp.RunCtx(relayCtx, debugLevel, "127.0.0.1", ports[0], password, tcpPorts)
	}()

	// Wait for relay to start accepting connections.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := tcp.PingServer(net.JoinHostPort("127.0.0.1", ports[0])); err == nil {
			break
		}
		if time.Now().After(deadline) {
			runtime.close()
			return nil, fmt.Errorf("local relay did not become ready on 127.0.0.1:%s", ports[0])
		}
		select {
		case <-ctx.Done():
			runtime.close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}

	return runtime, nil
}

func applyCommonCrocOptions(ops *croc.Options) {
	croc.Debug(false)
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		ops.DisplayName = hostname
	} else {
		ops.DisplayName = "unknown"
	}
	ops.Debug = false
	ops.DisableLocal = true
	ops.OnlyLocal = false
	ops.RelayAddress = "127.0.0.1"
	ops.RelayAddress6 = ""
	ops.Curve = "p256"
	ops.HashAlgorithm = "xxhash"
}

func addTunnelFlags(cmd *cobra.Command, t *tunnelOptions) {
	cmd.Flags().StringVarP(&t.URL, "url", "u", defaultWSURL, "Relay linksocks server URL")
	cmd.Flags().IntVar(&t.Threads, "threads", defaultThreads, "Number of parallel transfer threads")
	cmd.Flags().BoolVar(&t.Debug, "debug", false, "Enable debug logs")
	cmd.Flags().StringVarP(&t.DirectMode, "direct-mode", "m", string(linksocks.DirectModeAuto), "Direct connection mode: auto, relay-only, or direct-only")
}
