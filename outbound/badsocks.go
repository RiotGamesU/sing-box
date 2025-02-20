package outbound

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/mux"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-shadowsocks2"
	"github.com/sagernet/sing-shadowsocks2/badsocks"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/uot"
)

var _ adapter.Outbound = (*Badsocks)(nil)

type Badsocks struct {
	myOutboundAdapter
	dialer          N.Dialer
	method          shadowsocks.Method
	serverAddr      M.Socksaddr
	uotClient       *uot.Client
	multiplexDialer *mux.Client
}

func NewBadsocks(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.BadsocksOutboundOptions) (*Badsocks, error) {
	method, err := badsocks.NewMethod(ctx, badsocks.MethodName, shadowsocks.MethodOptions{
		Password: options.Password,
	})
	if err != nil {
		return nil, err
	}
	outbound := &Badsocks{
		myOutboundAdapter: myOutboundAdapter{
			protocol: C.TypeShadowsocks,
			network:  []string{N.NetworkTCP, N.NetworkUDP},
			router:   router,
			logger:   logger,
			tag:      tag,
		},
		dialer:     dialer.New(router, options.DialerOptions),
		method:     method,
		serverAddr: options.ServerOptions.Build(),
	}
	outbound.multiplexDialer, err = mux.NewClientWithOptions((*badsocksDialer)(outbound), common.PtrValueOrDefault(options.MultiplexOptions))
	if err != nil {
		return nil, err
	}
	if outbound.multiplexDialer == nil {
		outbound.uotClient = &uot.Client{
			Dialer: (*badsocksDialer)(outbound),
		}
	}
	return outbound, nil
}

func (h *Badsocks) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.AppendContext(ctx)
	metadata.Outbound = h.tag
	metadata.Destination = destination
	if h.multiplexDialer == nil {
		switch N.NetworkName(network) {
		case N.NetworkTCP:
			h.logger.InfoContext(ctx, "outbound connection to ", destination)
		case N.NetworkUDP:
			h.logger.InfoContext(ctx, "outbound connect packet connection to ", destination)
			return h.uotClient.DialContext(ctx, network, destination)
		}
		return (*badsocksDialer)(h).DialContext(ctx, network, destination)
	} else {
		switch N.NetworkName(network) {
		case N.NetworkTCP:
			h.logger.InfoContext(ctx, "outbound multiplex connection to ", destination)
		case N.NetworkUDP:
			h.logger.InfoContext(ctx, "outbound multiplex packet connection to ", destination)
		}
		return h.multiplexDialer.DialContext(ctx, network, destination)
	}
}

func (h *Badsocks) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.AppendContext(ctx)
	metadata.Outbound = h.tag
	metadata.Destination = destination
	if h.multiplexDialer == nil {
		h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
		return h.uotClient.ListenPacket(ctx, destination)
	} else {
		h.logger.InfoContext(ctx, "outbound multiplex packet connection to ", destination)
		return h.multiplexDialer.ListenPacket(ctx, destination)
	}
}

func (h *Badsocks) NewConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	return NewConnection(ctx, h, conn, metadata)
}

func (h *Badsocks) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	return NewPacketConnection(ctx, h, conn, metadata)
}

func (h *Badsocks) InterfaceUpdated() error {
	if h.multiplexDialer != nil {
		h.multiplexDialer.Reset()
	}
	return nil
}

func (h *Badsocks) Close() error {
	return common.Close(common.PtrOrNil(h.multiplexDialer))
}

var _ N.Dialer = (*badsocksDialer)(nil)

type badsocksDialer Badsocks

func (h *badsocksDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.AppendContext(ctx)
	metadata.Outbound = h.tag
	metadata.Destination = destination
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		outConn, err := h.dialer.DialContext(ctx, N.NetworkTCP, h.serverAddr)
		if err != nil {
			return nil, err
		}
		return h.method.DialEarlyConn(outConn, destination), nil
	case N.NetworkUDP:
		outConn, err := h.dialer.DialContext(ctx, N.NetworkUDP, h.serverAddr)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(h.method.DialPacketConn(outConn), destination), nil
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
}

func (h *badsocksDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.AppendContext(ctx)
	metadata.Outbound = h.tag
	metadata.Destination = destination
	outConn, err := h.dialer.DialContext(ctx, N.NetworkUDP, h.serverAddr)
	if err != nil {
		return nil, err
	}
	return h.method.DialPacketConn(outConn), nil
}
