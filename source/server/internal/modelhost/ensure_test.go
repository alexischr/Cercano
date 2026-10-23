package modelhost

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"cercano/source/server/internal/localruntime"
	"cercano/source/server/internal/localruntime/localruntimetest"
	"cercano/source/server/pkg/proto"
)

// startHost runs a real Server on a loopback listener and returns its record.
func startHost(t *testing.T) Record {
	t.Helper()
	mgr := localruntime.NewManager()
	mgr.RegisterProvider(localruntimetest.NewFakeProvider("fake"))
	srv := NewServer(mgr, ServerOptions{Token: "tok", Version: "v-test"})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer()
	srv.Register(g)
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	return Record{PID: os.Getpid(), Addr: lis.Addr().String(), Token: "tok", APIVersion: APIVersion, Version: "v-test", StartedAt: time.Now().UTC()}
}

func testOpts(dir string, launch func(context.Context) error) EnsureOptions {
	return EnsureOptions{RunDir: dir, ClientVersion: "c1", Launch: launch, Timeout: 5 * time.Second}
}

func TestEnsureReusesLiveHost(t *testing.T) {
	dir := t.TempDir()
	rec := startHost(t)
	if err := WriteRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	got, err := Ensure(context.Background(), testOpts(dir, func(context.Context) error { launches.Add(1); return nil }))
	if err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 0 || got.Addr != rec.Addr {
		t.Fatalf("launches=%d got=%+v", launches.Load(), got)
	}
}

func TestEnsureLaunchesWhenNoRecord(t *testing.T) {
	dir := t.TempDir()
	var launches atomic.Int32
	var want Record
	got, err := Ensure(context.Background(), testOpts(dir, func(context.Context) error {
		launches.Add(1)
		want = startHost(t)
		return WriteRecord(dir, want)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 1 || got.Addr != want.Addr {
		t.Fatalf("launches=%d got=%+v want=%+v", launches.Load(), got, want)
	}
}

// oldHost reports a different api_version and removes its record on Shutdown.
type oldHost struct {
	proto.UnimplementedModelHostServer
	dir      string
	shutdown atomic.Int32
}

func (o *oldHost) Hello(context.Context, *proto.HelloRequest) (*proto.HelloResponse, error) {
	return &proto.HelloResponse{ApiVersion: APIVersion + 1, Version: "old", Pid: 4242}, nil
}

func (o *oldHost) Shutdown(context.Context, *proto.ShutdownRequest) (*proto.ShutdownResponse, error) {
	o.shutdown.Add(1)
	_ = RemoveRecord(o.dir, 4242)
	return &proto.ShutdownResponse{}, nil
}

func TestEnsureReplacesIncompatibleHost(t *testing.T) {
	dir := t.TempDir()
	old := &oldHost{dir: dir}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := grpc.NewServer()
	proto.RegisterModelHostServer(g, old)
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	if err := WriteRecord(dir, Record{PID: 4242, Addr: lis.Addr().String(), Token: "tok", APIVersion: APIVersion + 1}); err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	var want Record
	got, err := Ensure(context.Background(), testOpts(dir, func(context.Context) error {
		launches.Add(1)
		if old.shutdown.Load() != 1 {
			t.Errorf("launch before shutdown")
		}
		want = startHost(t)
		return WriteRecord(dir, want)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if old.shutdown.Load() != 1 || launches.Load() != 1 || got.Addr != want.Addr {
		t.Fatalf("shutdown=%d launches=%d got=%+v", old.shutdown.Load(), launches.Load(), got)
	}
}

func TestConcurrentEnsureLaunchesOnce(t *testing.T) {
	dir := t.TempDir()
	var launches atomic.Int32
	launch := func(context.Context) error {
		launches.Add(1)
		rec := startHost(t)
		rec.PID = 777
		time.Sleep(200 * time.Millisecond)
		return WriteRecord(dir, rec)
	}
	var wg sync.WaitGroup
	pids := make([]int, 8)
	errs := make([]error, 8)
	for i := range pids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := Ensure(context.Background(), testOpts(dir, launch))
			pids[i], errs[i] = r.PID, err
		}()
	}
	wg.Wait()
	for i := range pids {
		if errs[i] != nil || pids[i] != 777 {
			t.Fatalf("goroutine %d: pid=%d err=%v", i, pids[i], errs[i])
		}
	}
	if launches.Load() != 1 {
		t.Fatalf("launches = %d, want 1", launches.Load())
	}
}

func TestEnsureIgnoresStaleRecord(t *testing.T) {
	dir := t.TempDir()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := lis.Addr().String()
	lis.Close()
	if err := WriteRecord(dir, Record{PID: 99, Addr: dead, Token: "tok", APIVersion: APIVersion}); err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	var want Record
	got, err := Ensure(context.Background(), testOpts(dir, func(context.Context) error {
		launches.Add(1)
		want = startHost(t)
		return WriteRecord(dir, want)
	}))
	if err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 1 || got.Addr != want.Addr {
		t.Fatalf("launches=%d got=%+v", launches.Load(), got)
	}
}

func TestEnsureWrongTokenIsNotLive(t *testing.T) {
	dir := t.TempDir()
	rec := startHost(t)
	rec.Token = "wrong"
	if err := WriteRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	_, err := Ensure(context.Background(), EnsureOptions{RunDir: dir, Timeout: 500 * time.Millisecond,
		Launch: func(context.Context) error { launches.Add(1); return nil }})
	if err == nil || launches.Load() != 1 {
		t.Fatalf("err=%v launches=%d", err, launches.Load())
	}
}

func TestEnsureTimesOutWhenHostNeverAppears(t *testing.T) {
	_, err := Ensure(context.Background(), EnsureOptions{RunDir: t.TempDir(), Timeout: 300 * time.Millisecond,
		Launch: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("want timeout error")
	}
}

func TestLaunchBinarySpawnsModelHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script launcher")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "fake-cercano")
	body := "#!/bin/sh\necho \"argv:$*\" > " + marker + "\necho hello-log\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := LaunchBinary(script)(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(marker)
		if err == nil && strings.TrimSpace(string(data)) == "argv:model-host" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker not written correctly: %q %v", data, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		log, _ := os.ReadFile(filepath.Join(RunDir(), "modelhost.log"))
		if strings.Contains(string(log), "hello-log") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("log missing output: %q", log)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
