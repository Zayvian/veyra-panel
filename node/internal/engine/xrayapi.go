package engine

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// xrayAPI talks to a running Xray over its gRPC API.
//
// The messages are encoded by hand rather than with Xray's generated Go types.
// Those types live in packages that also import the rest of Xray, and Xray and
// sing-box depend on different versions of the same libraries (quic-go, uTLS
// and more): one go.mod holding both would pin one of them to a version it was
// never built against. The handful of messages used here is small and has been
// stable for years, and the API is Xray's supported interface for exactly this.
//
// Field numbers are from Xray's .proto files: app/proxyman/command,
// app/stats/command, common/protocol/user, common/serial/typed_message and
// proxy/vless/account.
type xrayAPI struct {
	conn *grpc.ClientConn
}

func dialXrayAPI(addr string) (*xrayAPI, error) {
	conn, err := grpc.NewClient("passthrough:///"+addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})))
	if err != nil {
		return nil, err
	}
	return &xrayAPI{conn: conn}, nil
}

func (a *xrayAPI) Close() error { return a.conn.Close() }

func (a *xrayAPI) call(ctx context.Context, method string, req []byte) ([]byte, error) {
	var resp []byte
	if err := a.conn.Invoke(ctx, method, &req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

const (
	xrayHandlerService = "/xray.app.proxyman.command.HandlerService/"
	xrayStatsService   = "/xray.app.stats.command.StatsService/"
	xrayRoutingService = "/xray.app.router.command.RoutingService/"
	xrayRouterConfig   = "xray.app.router.Config"

	xrayAddUserOp    = "xray.app.proxyman.command.AddUserOperation"
	xrayRemoveUserOp = "xray.app.proxyman.command.RemoveUserOperation"
	xrayVLESSAccount = "xray.proxy.vless.Account"
)

// AddVLESSUser adds one user to a running VLESS inbound. Connections of every
// other user are left alone.
func (a *xrayAPI) AddVLESSUser(ctx context.Context, tag, email, id, flow string) error {
	// vless.Account { string id = 1; string flow = 2; }
	var account []byte
	account = appendString(account, 1, id)
	account = appendString(account, 2, flow)

	// protocol.User { uint32 level = 1; string email = 2; TypedMessage account = 3; }
	// Level stays 0: the node's policy turns on per-user statistics for level 0.
	var user []byte
	user = appendString(user, 2, email)
	user = appendMessage(user, 3, typedMessage(xrayVLESSAccount, account))

	// AddUserOperation { protocol.User user = 1; }
	var op []byte
	op = appendMessage(op, 1, user)
	return a.alterInbound(ctx, tag, typedMessage(xrayAddUserOp, op))
}

// RemoveUser removes a user from a running inbound by email, which is what
// Xray keys users on. Live connections of that user are closed by Xray.
func (a *xrayAPI) RemoveUser(ctx context.Context, tag, email string) error {
	// RemoveUserOperation { string email = 1; }
	var op []byte
	op = appendString(op, 1, email)
	return a.alterInbound(ctx, tag, typedMessage(xrayRemoveUserOp, op))
}

func (a *xrayAPI) alterInbound(ctx context.Context, tag string, op []byte) error {
	// AlterInboundRequest { string tag = 1; TypedMessage operation = 2; }
	var req []byte
	req = appendString(req, 1, tag)
	req = appendMessage(req, 2, op)
	_, err := a.call(ctx, xrayHandlerService+"AlterInbound", req)
	return err
}

// QueryStats returns every counter whose name contains pattern, without
// resetting it: see readTotals for why counters are never reset.
func (a *xrayAPI) QueryStats(ctx context.Context, pattern string) (map[string]int64, error) {
	// QueryStatsRequest { string pattern = 1; bool reset = 2; }
	var req []byte
	req = appendString(req, 1, pattern)
	resp, err := a.call(ctx, xrayStatsService+"QueryStats", req)
	if err != nil {
		return nil, err
	}

	// QueryStatsResponse { repeated Stat stat = 1; }
	// Stat { string name = 1; int64 value = 2; }
	out := map[string]int64{}
	err = eachField(resp, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 1 || typ != protowire.BytesType {
			return nil
		}
		var name string
		var value int64
		err := eachField(v, func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error {
			switch {
			case num == 1 && typ == protowire.BytesType:
				name = string(v)
			case num == 2 && typ == protowire.VarintType:
				value = int64(n)
			}
			return nil
		})
		if err != nil {
			return err
		}
		out[name] = value
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("decode QueryStatsResponse: %w", err)
	}
	return out, nil
}

// OnlineUsers lists the users Xray counts as connected right now.
//
// Xray names them by their online counter, "user>>>alice>>>online", and only
// tracks them when the node's policy turns statsUserOnline on.
func (a *xrayAPI) OnlineUsers(ctx context.Context) ([]string, error) {
	resp, err := a.call(ctx, xrayStatsService+"GetAllOnlineUsers", nil)
	if err != nil {
		return nil, err
	}
	// GetAllOnlineUsersResponse { repeated string users = 1; }
	var out []string
	err = eachField(resp, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 1 || typ != protowire.BytesType {
			return nil
		}
		name := string(v)
		name = strings.TrimPrefix(name, userPrefix)
		name = strings.TrimSuffix(name, ">>>online")
		if name != "" {
			out = append(out, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("decode GetAllOnlineUsersResponse: %w", err)
	}
	return out, nil
}

// OnlineIPs lists the source addresses a user has live connections from, with
// when each was last seen. A user Xray has never seen online is not an error:
// they simply have none.
func (a *xrayAPI) OnlineIPs(ctx context.Context, user string) (map[string]time.Time, error) {
	// GetStatsRequest { string name = 1; bool reset = 2; }
	var req []byte
	req = appendString(req, 1, userPrefix+user+">>>online")
	resp, err := a.call(ctx, xrayStatsService+"GetStatsOnlineIpList", req)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// GetStatsOnlineIpListResponse { string name = 1; map<string, int64> ips = 2; }
	// A map entry is a message { string key = 1; int64 value = 2; }.
	out := map[string]time.Time{}
	err = eachField(resp, func(num protowire.Number, typ protowire.Type, v []byte, _ uint64) error {
		if num != 2 || typ != protowire.BytesType {
			return nil
		}
		var ip string
		var seen int64
		err := eachField(v, func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error {
			switch {
			case num == 1 && typ == protowire.BytesType:
				ip = string(v)
			case num == 2 && typ == protowire.VarintType:
				seen = int64(n)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if ip != "" {
			out[ip] = time.Unix(seen, 0)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("decode GetStatsOnlineIpListResponse: %w", err)
	}
	return out, nil
}

// BlockSources adds a routing rule, named ruleTag, that sends every new
// connection of user from one of addrs to the node's block outbound.
// Connections already open are not affected: Xray has no way to close one
// from outside.
func (a *xrayAPI) BlockSources(ctx context.Context, ruleTag, user string, addrs []netip.Addr) error {
	// RoutingRule { string tag = 1; repeated GeoIP source_geoip = 11;
	//               repeated string user_email = 7; string rule_tag = 19; }
	// GeoIP { repeated CIDR cidr = 2; }  CIDR { bytes ip = 1; uint32 prefix = 2; }
	var geoip []byte
	for _, addr := range addrs {
		ip := addr.Unmap().AsSlice()
		var cidr []byte
		cidr = protowire.AppendTag(cidr, 1, protowire.BytesType)
		cidr = protowire.AppendBytes(cidr, ip)
		cidr = protowire.AppendTag(cidr, 2, protowire.VarintType)
		cidr = protowire.AppendVarint(cidr, uint64(len(ip)*8))
		geoip = appendMessage(geoip, 2, cidr)
	}
	var rule []byte
	rule = appendString(rule, 1, xrayBlockOutbound)
	rule = appendString(rule, 7, user)
	rule = appendMessage(rule, 11, geoip)
	rule = appendString(rule, 19, ruleTag)

	// router.Config { repeated RoutingRule rule = 2; }
	var cfg []byte
	cfg = appendMessage(cfg, 2, rule)

	// AddRuleRequest { TypedMessage config = 1; bool shouldAppend = 2; }
	// Appended: the rules already there are the panel's policy, which only
	// blocks, so nothing ahead of this rule can let the address through.
	var req []byte
	req = appendMessage(req, 1, typedMessage(xrayRouterConfig, cfg))
	req = protowire.AppendTag(req, 2, protowire.VarintType)
	req = protowire.AppendVarint(req, 1)
	_, err := a.call(ctx, xrayRoutingService+"AddRule", req)
	return err
}

// AddDomainRule appends a routing rule, named ruleTag, that sends connections
// to any of suffixes, or a name under one, to outbound. The domain is the one
// sniffing found, so the inbounds must sniff.
func (a *xrayAPI) AddDomainRule(ctx context.Context, ruleTag, outbound string, suffixes []string) error {
	// RoutingRule { string tag = 1; repeated Domain domain = 2; string rule_tag = 19; }
	// Domain { Type type = 1; string value = 2; }, where Type Domain = 2 is
	// the suffix match a "domain:" entry in a configuration file makes.
	var rule []byte
	rule = appendString(rule, 1, outbound)
	for _, d := range suffixes {
		var dom []byte
		dom = protowire.AppendTag(dom, 1, protowire.VarintType)
		dom = protowire.AppendVarint(dom, 2)
		dom = appendString(dom, 2, d)
		rule = appendMessage(rule, 2, dom)
	}
	rule = appendString(rule, 19, ruleTag)

	var cfg []byte
	cfg = appendMessage(cfg, 2, rule)
	var req []byte
	req = appendMessage(req, 1, typedMessage(xrayRouterConfig, cfg))
	req = protowire.AppendTag(req, 2, protowire.VarintType)
	req = protowire.AppendVarint(req, 1)
	_, err := a.call(ctx, xrayRoutingService+"AddRule", req)
	return err
}

// RemoveRule removes the routing rule named ruleTag. One that is not there is
// already removed.
func (a *xrayAPI) RemoveRule(ctx context.Context, ruleTag string) error {
	// RemoveRuleRequest { string ruleTag = 1; }
	var req []byte
	req = appendString(req, 1, ruleTag)
	_, err := a.call(ctx, xrayRoutingService+"RemoveRule", req)
	if err != nil && strings.Contains(err.Error(), "not found") {
		return nil
	}
	return err
}

// typedMessage encodes Xray's TypedMessage { string type = 1; bytes value = 2; },
// its way of carrying a message whose type the receiver picks by name.
func typedMessage(typ string, value []byte) []byte {
	var b []byte
	b = appendString(b, 1, typ)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

// appendString leaves an empty string out, as proto3 does.
func appendString(b []byte, num protowire.Number, s string) []byte {
	if s == "" {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func appendMessage(b []byte, num protowire.Number, m []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, m)
}

// eachField walks the top level of an encoded message. For length-delimited
// fields v is the payload; for varints n is the value.
func eachField(b []byte, fn func(num protowire.Number, typ protowire.Type, v []byte, n uint64) error) error {
	for len(b) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(b)
		if tagLen < 0 {
			return protowire.ParseError(tagLen)
		}
		b = b[tagLen:]
		var (
			v   []byte
			n   uint64
			fl  int
			err error
		)
		switch typ {
		case protowire.BytesType:
			v, fl = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			n, fl = protowire.ConsumeVarint(b)
		default:
			fl = protowire.ConsumeFieldValue(num, typ, b)
		}
		if fl < 0 {
			return protowire.ParseError(fl)
		}
		b = b[fl:]
		if err = fn(num, typ, v, n); err != nil {
			return err
		}
	}
	return nil
}

// rawCodec passes already-encoded protobuf through gRPC untouched.
type rawCodec struct{}

var _ encoding.Codec = rawCodec{}

func (rawCodec) Name() string { return "proto" }

func (rawCodec) Marshal(v any) ([]byte, error) {
	p, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("raw codec: cannot marshal %T", v)
	}
	return *p, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("raw codec: cannot unmarshal into %T", v)
	}
	*p = append((*p)[:0], data...)
	return nil
}
