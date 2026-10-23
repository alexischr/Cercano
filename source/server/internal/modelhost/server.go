package modelhost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"cercano/source/server/internal/localruntime"
	"cercano/source/server/pkg/proto"
)

// tokenHeader is the metadata key carrying the shared secret.
const tokenHeader = "x-cercano-modelhost-token"

// defaultIdleTimeout is how long the host stays up with no attached client.
const defaultIdleTimeout = 5 * time.Minute

// streamBuffer bounds the per-stream event queue. A stream that falls this far
// behind loses events instead of stalling the manager.
const streamBuffer = 128

// ServerOptions configures a Server.
type ServerOptions struct {
	// Token is the shared secret every RPC must present in metadata.
	Token string
	// IdleTimeout is how long the server tolerates having no attached stream
	// before stopping all instances and calling OnIdle. Default 5 minutes.
	IdleTimeout time.Duration
	// Now returns the current time. Default time.Now.
	Now func() time.Time
	// OnIdle is called once, after StopAll, when the idle timeout expires.
	OnIdle func()
	// Version is reported by Hello.
	Version string
	// OnShutdown is called after StopAll when a client calls Shutdown.
	OnShutdown func()
}

// Server exposes a localruntime.Manager over gRPC and shares running
// instances between clients through holds.
type Server struct {
	proto.UnimplementedModelHostServer

	mgr   localruntime.Manager
	opts  ServerOptions
	holds *holdTable

	// lifeMu serializes instance lifecycle operations (Start, Stop, Restart,
	// hold release, StopAll) so dedupe and hold bookkeeping stay consistent.
	lifeMu sync.Mutex

	mu        sync.Mutex
	streams   map[*stream]struct{}
	clients   map[string]int // attached stream count per client ID
	idleSince time.Time
	idleFired bool
}

type stream struct {
	ch chan *proto.ManagerEvent
}

// NewServer wraps m. It registers itself as an observer of m once.
func NewServer(m localruntime.Manager, opts ServerOptions) *Server {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Server{
		mgr:     m,
		opts:    opts,
		holds:   newHoldTable(),
		streams: map[*stream]struct{}{},
		clients: map[string]int{},
	}
	s.idleSince = opts.Now()
	m.RegisterObserver(s)
	return s
}

// Register adds the ModelHost service to g.
func (s *Server) Register(g *grpc.Server) {
	proto.RegisterModelHostServer(g, s)
}

// Run checks for idleness periodically until ctx is done.
func (s *Server) Run(ctx context.Context) {
	interval := s.opts.IdleTimeout / 4
	if interval > time.Second {
		interval = time.Second
	}
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.checkIdle(ctx)
		}
	}
}

// checkIdle stops everything and calls OnIdle once if no stream has been
// attached for IdleTimeout.
func (s *Server) checkIdle(ctx context.Context) {
	s.mu.Lock()
	expired := len(s.streams) == 0 && !s.idleFired &&
		s.opts.Now().Sub(s.idleSince) >= s.opts.IdleTimeout
	if expired {
		s.idleFired = true
	}
	s.mu.Unlock()
	if !expired {
		return
	}
	if err := s.StopAll(ctx); err != nil {
		log.Printf("modelhost: idle stop: %v", err)
	}
	if s.opts.OnIdle != nil {
		s.opts.OnIdle()
	}
}

// StopAll stops every live instance and drops all holds.
func (s *Server) StopAll(ctx context.Context) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	instances, err := s.mgr.Instances(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, inst := range instances {
		if !liveInstance(inst) {
			continue
		}
		if err := s.mgr.Stop(ctx, localruntime.StopRequest{InstanceID: inst.ID}); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", inst.ID, err))
		}
	}
	s.holds.clear()
	return errors.Join(errs...)
}

func liveInstance(i localruntime.InstanceRecord) bool {
	switch i.State {
	case localruntime.InstanceStarting, localruntime.InstanceRunning, localruntime.InstanceHealthy,
		localruntime.InstanceDegraded, localruntime.InstanceUnhealthy:
		return true
	}
	return false
}

// authorize checks the token in the incoming metadata.
func (s *Server) authorize(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	var got string
	if vals := md.Get(tokenHeader); len(vals) > 0 {
		got = vals[0]
	}
	if s.opts.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid model host token")
	}
	return nil
}

// Hello implements the version handshake.
func (s *Server) Hello(ctx context.Context, _ *proto.HelloRequest) (*proto.HelloResponse, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return &proto.HelloResponse{ApiVersion: APIVersion, Version: s.opts.Version, Pid: int32(os.Getpid())}, nil
}

// Shutdown stops all instances, then calls OnShutdown.
func (s *Server) Shutdown(ctx context.Context, _ *proto.ShutdownRequest) (*proto.ShutdownResponse, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	err := s.StopAll(ctx)
	if s.opts.OnShutdown != nil {
		s.opts.OnShutdown()
	}
	if err != nil {
		return nil, encodeError(err)
	}
	return &proto.ShutdownResponse{}, nil
}

// Attach streams manager events to the client until it disconnects, then
// releases the client's holds.
func (s *Server) Attach(req *proto.AttachRequest, srv grpc.ServerStreamingServer[proto.ManagerEvent]) error {
	ctx := srv.Context()
	if err := s.authorize(ctx); err != nil {
		return err
	}
	st := s.addStream(req.GetClientId())
	defer func() {
		if s.removeStream(req.GetClientId(), st) {
			s.releaseClient(req.GetClientId())
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-st.ch:
			if err := srv.Send(ev); err != nil {
				return err
			}
		}
	}
}

func (s *Server) addStream(clientID string) *stream {
	st := &stream{ch: make(chan *proto.ManagerEvent, streamBuffer)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[st] = struct{}{}
	s.clients[clientID]++
	s.idleFired = false
	return st
}

// removeStream detaches st and reports whether clientID has no streams left.
func (s *Server) removeStream(clientID string, st *stream) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.streams, st)
	s.clients[clientID]--
	gone := s.clients[clientID] <= 0
	if gone {
		delete(s.clients, clientID)
	}
	if len(s.streams) == 0 {
		s.idleSince = s.opts.Now()
	}
	return gone
}

func (s *Server) attachedClients(clientID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients[clientID]
}

// releaseClient drops all of clientID's holds, stopping instances left unheld.
func (s *Server) releaseClient(clientID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	for _, id := range s.holds.heldBy(clientID) {
		if err := s.releaseLocked(ctx, clientID, id); err != nil {
			log.Printf("modelhost: release %s for client %s: %v", id, clientID, err)
		}
	}
}

// releaseLocked removes clientID's hold on id and stops the instance when no
// holds remain. lifeMu must be held.
func (s *Server) releaseLocked(ctx context.Context, clientID, id string) error {
	if s.holds.remove(clientID, id) > 0 {
		return nil
	}
	return s.mgr.Stop(ctx, localruntime.StopRequest{InstanceID: id})
}

// OnDownloadStateChange implements localruntime.Observer.
func (s *Server) OnDownloadStateChange(ev localruntime.DownloadEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.broadcast(&proto.ManagerEvent{Event: &proto.ManagerEvent_DownloadEventJson{DownloadEventJson: string(b)}})
}

// OnInstanceStateChange implements localruntime.Observer.
func (s *Server) OnInstanceStateChange(ev localruntime.InstanceEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.broadcast(&proto.ManagerEvent{Event: &proto.ManagerEvent_InstanceEventJson{InstanceEventJson: string(b)}})
}

// broadcast queues ev on every stream without blocking; a full stream drops it.
func (s *Server) broadcast(ev *proto.ManagerEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for st := range s.streams {
		select {
		case st.ch <- ev:
		default:
			log.Printf("modelhost: dropping event for slow stream")
		}
	}
}

// Call dispatches a localruntime.Manager method by name.
func (s *Server) Call(ctx context.Context, req *proto.CallRequest) (*proto.CallResponse, error) {
	if err := s.authorize(ctx); err != nil {
		return nil, err
	}
	out, err := s.dispatch(ctx, req.GetClientId(), req.GetMethod(), []byte(req.GetPayloadJson()))
	if err != nil {
		return nil, encodeError(err)
	}
	return &proto.CallResponse{PayloadJson: out}, nil
}

// decode unmarshals payload into v; an empty payload leaves v zero.
func decode(payload []byte, v any) error {
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return status.Errorf(codes.InvalidArgument, "bad payload: %v", err)
	}
	return nil
}

func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", status.Errorf(codes.Internal, "encode response: %v", err)
	}
	return string(b), nil
}

func (s *Server) dispatch(ctx context.Context, clientID, method string, payload []byte) (string, error) {
	switch method {
	case "Providers":
		return encode(s.mgr.Providers())
	case "Inventory":
		v, err := s.mgr.Inventory(ctx)
		return reply(v, err)
	case "Instances":
		v, err := s.mgr.Instances(ctx)
		return reply(v, err)
	case "Endpoints":
		v, err := s.mgr.Endpoints(ctx)
		return reply(v, err)
	case "Start":
		var r localruntime.StartRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.start(ctx, clientID, r)
		return reply(v, err)
	case "Stop":
		var r localruntime.StopRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		return "", s.stop(ctx, clientID, r)
	case "Restart":
		var r localruntime.RestartRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.restart(ctx, clientID, r)
		return reply(v, err)
	case "UpdateInstance":
		var r localruntime.InstanceRecord
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		s.mgr.UpdateInstance(r)
		return "", nil
	case "DownloadModel":
		var r localruntime.DownloadRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.mgr.DownloadModel(ctx, r)
		return reply(v, err)
	case "EnsureModelsPresent":
		var r struct {
			Runtime string
			Want    []string
		}
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		return "", s.mgr.EnsureModelsPresent(ctx, r.Runtime, r.Want)
	case "ResolveOpenModel":
		var r struct {
			Runtime string
			Want    string
		}
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.mgr.ResolveOpenModel(ctx, r.Runtime, r.Want)
		// ErrModelNotPresent still returns the record; the sentinel travels
		// in the error and the client re-fetches by name if it needs it.
		return reply(v, err)
	case "CancelDownload":
		var r localruntime.DownloadRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.mgr.CancelDownload(ctx, r)
		return reply(v, err)
	case "DeleteModel":
		var r localruntime.DeleteModelRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		return "", s.mgr.DeleteModel(ctx, r)
	case "Status":
		v, err := s.mgr.Status(ctx)
		return reply(v, err)
	case "Logs":
		var r localruntime.LogRequest
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		v, err := s.mgr.Logs(ctx, r)
		return reply(v, err)
	case "WriteLog":
		var r localruntime.LogEntry
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		s.mgr.WriteLog(r)
		return "", nil
	case "EnrollDownload":
		enroller, ok := s.mgr.(localruntime.DownloadEnroller)
		if !ok {
			return "", status.Error(codes.FailedPrecondition, "model host manager does not support EnrollDownload")
		}
		var r localruntime.ModelRecord
		if err := decode(payload, &r); err != nil {
			return "", err
		}
		enroller.EnrollDownload(r)
		return "", nil
	}
	return "", status.Errorf(codes.Unimplemented, "unknown model host method %q", method)
}

// reply encodes a successful result, or returns err untouched. A nil pointer
// result with a nil error encodes as JSON null.
func reply(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return encode(v)
}

// start shares a running instance for the same runtime and model, otherwise
// starts one, and records the client's hold either way.
func (s *Server) start(ctx context.Context, clientID string, req localruntime.StartRequest) (*localruntime.InstanceRecord, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	instances, err := s.mgr.Instances(ctx)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if liveInstance(inst) && inst.Runtime == req.Runtime && inst.ModelID == req.ModelID {
			inst := inst
			s.holds.add(clientID, inst.ID)
			return &inst, nil
		}
	}
	inst, err := s.mgr.Start(ctx, req)
	if err != nil {
		return nil, err
	}
	s.holds.add(clientID, inst.ID)
	return inst, nil
}

// stop releases the client's hold and stops the instance once unheld.
func (s *Server) stop(ctx context.Context, clientID string, req localruntime.StopRequest) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	return s.releaseLocked(ctx, clientID, req.InstanceID)
}

// restart restarts for every holder; holds follow the instance if its ID changes.
func (s *Server) restart(ctx context.Context, clientID string, req localruntime.RestartRequest) (*localruntime.InstanceRecord, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	inst, err := s.mgr.Restart(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.InstanceID != "" {
		s.holds.move(req.InstanceID, inst.ID)
	}
	if s.holds.count(inst.ID) == 0 {
		// A restart that degenerated into a fresh start still needs an owner.
		s.holds.add(clientID, inst.ID)
	}
	return inst, nil
}
