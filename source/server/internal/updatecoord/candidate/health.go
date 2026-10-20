package candidate

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"cercano/source/server/internal/updatecoord/nativeprocess"
	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Observation is evidence from separate, non-atomic samples, NOT authenticated
// health or permission to complete an update, restore a selection, or clean up.
// Even Consistent samples cannot prove the RPC peer is the candidate: another
// listener may share the endpoint. Client reconnection is not observed here.
type Observation struct {
	Status          ObservationStatus
	Reason          string
	Version         string
	ReportedPID     int32
	Ready           bool
	NotReadyReasons []string
}
type ObservationStatus string

const (
	Unknown      ObservationStatus = "unknown"
	Inconsistent ObservationStatus = "inconsistent"
	NotReady     ObservationStatus = "not-ready"
	Consistent   ObservationStatus = "consistent-observations-only"
)
const observationBudget = 5 * time.Second

var ErrObservationRequest = errors.New("candidate: invalid health observation request")

type healthSamples struct {
	identity func() (nativeprocess.Identity, error)
	listener func(nativeprocess.Identity, netip.AddrPort) (bool, error)
	health   func(context.Context, netip.AddrPort) (*proto.GetAgentHealthResponse, error)
}

// ObserveHealth performs one read-only attempt against an explicit loopback
// endpoint, with a five-second network/context budget (or the caller's earlier
// deadline). Synchronous native inspections cannot be preempted; context is
// checked between samples and before accepting the result. It never discovers,
// launches, retries, or stops an agent.
// Errors/unsupported probes are unknown, never proof that the candidate stopped.
func (c *Candidate) ObserveHealth(ctx context.Context, endpoint netip.AddrPort, expectedVersion string) (Observation, error) {
	return observeHealth(ctx, endpoint, expectedVersion, healthSamples{c.Identity, nativeprocess.HoldsListener, readHealth})
}
func observeHealth(ctx context.Context, endpoint netip.AddrPort, version string, s healthSamples) (Observation, error) {
	unknown := func(reason string) Observation { return Observation{Status: Unknown, Reason: reason} }
	if ctx == nil || !endpoint.IsValid() || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" || version == "" || len(version) > 256 || strings.TrimSpace(version) != version || strings.ContainsAny(version, "\x00\r\n") {
		return unknown("invalid-request"), ErrObservationRequest
	}
	ctx, cancel := context.WithTimeout(ctx, observationBudget)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return unknown("canceled"), err
	}
	before, err := s.identity()
	if err != nil {
		return unknown("lifetime-unavailable"), err
	}
	held, err := s.listener(before, endpoint)
	if err != nil {
		return unknown("listener-unavailable"), err
	}
	if !held {
		return unknown("listener-not-observed"), nil
	}
	if err = ctx.Err(); err != nil {
		return unknown("canceled"), err
	}
	response, err := s.health(ctx, endpoint)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return unknown("health-unsupported"), err
		}
		return unknown("health-unavailable"), err
	}
	// Reobserve even a negative response; never preserve a positive claim when
	// the later lifetime or socket observation is uncertain.
	held, err = s.listener(before, endpoint)
	if err != nil {
		return unknown("listener-unavailable"), err
	}
	if !held {
		return unknown("listener-not-observed"), nil
	}
	after, err := s.identity()
	if err != nil {
		return unknown("lifetime-unavailable"), err
	}
	if err = ctx.Err(); err != nil {
		return unknown("canceled"), err
	}
	if !before.SameProcess(after) {
		return unknown("lifetime-changed"), nil
	}
	if response == nil {
		return unknown("empty-health-response"), nil
	}
	out := Observation{Status: Inconsistent, Version: response.Version, ReportedPID: response.Pid, Ready: response.Ready, NotReadyReasons: append([]string(nil), response.NotReadyReasons...)}
	switch {
	case response.Pid <= 0 || int(response.Pid) != before.PID:
		out.Reason = "reported-pid-mismatch"
	case response.Version != version:
		out.Reason = "reported-version-mismatch"
	case response.Ready && len(response.NotReadyReasons) != 0:
		out.Reason = "contradictory-readiness"
	case !response.Ready:
		out.Status = NotReady
		out.Reason = "reported-not-ready"
	default:
		out.Status = Consistent
		out.Reason = "peer-not-authenticated"
	}
	return out, nil
}
func readHealth(ctx context.Context, endpoint netip.AddrPort) (*proto.GetAgentHealthResponse, error) {
	// Numeric loopback endpoint only, no resolver, proxy, auto-launch or reconnect.
	conn, err := grpc.DialContext(ctx, endpoint.String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy(), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(16<<10), grpc.MaxCallSendMsgSize(1024)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return proto.NewAgentClient(conn).GetAgentHealth(ctx, &proto.GetAgentHealthRequest{})
}
