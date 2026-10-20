package candidate

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"cercano/source/server/internal/updatecoord/nativeprocess"
	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHealthObservationSamples(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:1234")
	for _, tc := range []struct {
		name   string
		want   ObservationStatus
		reason string
	}{
		{"consistent", Consistent, "peer-not-authenticated"}, {"not-ready", NotReady, "reported-not-ready"},
		{"pid", Inconsistent, "reported-pid-mismatch"}, {"version", Inconsistent, "reported-version-mismatch"},
		{"contradictory", Inconsistent, "contradictory-readiness"}, {"nil", Unknown, "empty-health-response"},
		{"unsupported", Unknown, "health-unsupported"}, {"rpc-error", Unknown, "health-unavailable"},
		{"pre-identity", Unknown, "lifetime-unavailable"}, {"post-identity", Unknown, "lifetime-unavailable"},
		{"changed", Unknown, "lifetime-changed"}, {"pre-listener", Unknown, "listener-not-observed"},
		{"post-listener", Unknown, "listener-not-observed"}, {"listener-error", Unknown, "listener-unavailable"},
		{"post-listener-error", Unknown, "listener-unavailable"}, {"cancel-after-rpc", Unknown, "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := nativeprocess.Identity{PID: 42, Executable: "fixture", StartTicks: 1}
			response := &proto.GetAgentHealthResponse{Version: "1.2.3", Pid: 42, Ready: true}
			var order []string
			identities, listeners := 0, 0
			s := healthSamples{
				identity: func() (nativeprocess.Identity, error) {
					order = append(order, "identity")
					identities++
					if tc.name == "pre-identity" || tc.name == "post-identity" && identities == 2 {
						return id, errors.New("uncertain")
					}
					if tc.name == "changed" && identities == 2 {
						id.StartTicks++
					}
					return id, nil
				},
				listener: func(got nativeprocess.Identity, ep netip.AddrPort) (bool, error) {
					order = append(order, "listener")
					listeners++
					if got.PID != 42 || ep != endpoint {
						t.Fatal("binding lost")
					}
					if tc.name == "listener-error" || tc.name == "post-listener-error" && listeners == 2 {
						return false, errors.New("uncertain")
					}
					return !(tc.name == "pre-listener" || tc.name == "post-listener" && listeners == 2), nil
				},
				health: func(ctx context.Context, ep netip.AddrPort) (*proto.GetAgentHealthResponse, error) {
					order = append(order, "health")
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > observationBudget {
						t.Fatal("unbounded query")
					}
					switch tc.name {
					case "not-ready":
						response.Ready = false
						response.NotReadyReasons = []string{"startup_incomplete"}
					case "pid":
						response.Pid++
					case "version":
						response.Version = "other"
					case "contradictory":
						response.NotReadyReasons = []string{"shutdown_begun"}
					case "nil":
						return nil, nil
					case "unsupported":
						return nil, status.Error(codes.Unimplemented, "old")
					case "rpc-error":
						return nil, errors.New("transport")
					case "cancel-after-rpc":
						cancel()
					}
					return response, nil
				},
			}
			got, _ := observeHealth(ctx, endpoint, "1.2.3", s)
			if got.Status != tc.want || got.Reason != tc.reason {
				t.Fatalf("got %+v", got)
			}
			if tc.name == "consistent" && !reflect.DeepEqual(order, []string{"identity", "listener", "health", "listener", "identity"}) {
				t.Fatal(order)
			}
			if tc.name == "pre-identity" || tc.name == "pre-listener" || tc.name == "listener-error" {
				for _, v := range order {
					if v == "health" {
						t.Fatal("queried unobserved endpoint")
					}
				}
			}
			if len(got.NotReadyReasons) > 0 {
				response.NotReadyReasons[0] = "mutated"
				if got.NotReadyReasons[0] == "mutated" {
					t.Fatal("aliased response")
				}
			}
		})
	}
}
func TestHealthObservationInvalidAndCanceled(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:1", "192.0.2.1:123", "127.0.0.1:0", "[::1%zone]:1"} {
		got, err := observeHealth(context.Background(), netip.MustParseAddrPort(addr), "1", healthSamples{})
		if !errors.Is(err, ErrObservationRequest) || got.Status != Unknown {
			t.Fatal(got, err)
		}
	}
	got, err := observeHealth(nil, netip.MustParseAddrPort("127.0.0.1:1"), "1", healthSamples{})
	if !errors.Is(err, ErrObservationRequest) || got.Status != Unknown {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = observeHealth(ctx, netip.MustParseAddrPort("127.0.0.1:1"), "1", healthSamples{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var c *Candidate
	got, err = c.ObserveHealth(context.Background(), netip.MustParseAddrPort("127.0.0.1:1"), "1")
	if !errors.Is(err, ErrIdentity) || got.Status != Unknown {
		t.Fatal(got, err)
	}
}

type healthFixture struct {
	proto.UnimplementedAgentServer
	response *proto.GetAgentHealthResponse
	block    bool
}

func (f *healthFixture) GetAgentHealth(ctx context.Context, _ *proto.GetAgentHealthRequest) (*proto.GetAgentHealthResponse, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.response, nil
}
func TestHealthObservationWire(t *testing.T) {
	for _, kind := range []string{"ready", "old", "oversized", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := grpc.NewServer()
			response := &proto.GetAgentHealthResponse{Version: "1.2.3", Pid: 42, Ready: true}
			if kind == "old" {
				proto.RegisterAgentServer(server, &proto.UnimplementedAgentServer{})
			} else {
				if kind == "oversized" {
					response.Version = string(make([]byte, 32<<10))
				}
				proto.RegisterAgentServer(server, &healthFixture{response: response, block: kind == "deadline"})
			}
			done := make(chan struct{})
			go func() { defer close(done); _ = server.Serve(listener) }()
			defer func() { server.Stop(); <-done }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := readHealth(ctx, listener.Addr().(*net.TCPAddr).AddrPort())
			switch kind {
			case "ready":
				if err != nil || got.Version != "1.2.3" {
					t.Fatal(got, err)
				}
			case "old":
				if status.Code(err) != codes.Unimplemented {
					t.Fatal(err)
				}
			case "oversized":
				if status.Code(err) != codes.ResourceExhausted {
					t.Fatal(err)
				}
			case "deadline":
				if status.Code(err) != codes.DeadlineExceeded {
					t.Fatal(err)
				}
			}
		})
	}
}
