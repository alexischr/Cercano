package main

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/modelhost"
)

func TestUpgradeRestartCommand(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		args                       []string
		cfgErr, exeErr, restartErr bool
		restarted                  bool
		wantCode, wantCalls        int
		message                    string
	}{
		{name: "help", args: []string{"--help"}, wantCode: 0, message: "Usage:"},
		{name: "invalid port", args: []string{"--address", "127.0.0.1:0"}, wantCode: 2},
		{name: "remote endpoint", args: []string{"--address", "192.0.2.1:9000"}, wantCode: 2},
		{name: "unbounded timeout", args: []string{"--timeout", "0s"}, wantCode: 2},
		{name: "extra arguments", args: []string{"surprise"}, wantCode: 2},
		{name: "configuration error", cfgErr: true, wantCode: 1},
		{name: "executable error", exeErr: true, wantCode: 1},
		{name: "absent", wantCode: 0, wantCalls: 1, message: "nothing started"},
		{name: "restarted", restarted: true, wantCode: 0, wantCalls: 1, message: "restarted using"},
		{name: "restart failure", restartErr: true, wantCode: 1, wantCalls: 1, message: "update may already be installed"},
		{name: "explicit address bypasses config", args: []string{"--address", "127.0.0.1:4242"}, cfgErr: true, wantCode: 0, wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			calls := 0
			cfgCalls := 0
			code := upgradeRestartCommand(tt.args, &output,
				func() (string, error) {
					cfgCalls++
					if tt.cfgErr {
						return "", errors.New("config error")
					}
					return "4242", nil
				},
				func() (string, error) {
					if tt.exeErr {
						return "", errors.New("exe error")
					}
					return "/fake/Cellar/cercano/2/bin/cercano", nil
				},
				func(context.Context) error { return nil },
				func(ctx context.Context, path string, endpoint netip.AddrPort) (bool, error) {
					calls++
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("no restart deadline")
					}
					if path != "/fake/Cellar/cercano/2/bin/cercano" || endpoint.String() != "127.0.0.1:4242" {
						t.Fatal("incorrect restart target")
					}
					if tt.restartErr {
						return false, errors.New("failure")
					}
					return tt.restarted, nil
				})
			if code != tt.wantCode || calls != tt.wantCalls {
				t.Fatalf("code=%d calls=%d output=%s", code, calls, output.String())
			}
			if !strings.Contains(output.String(), tt.message) {
				t.Fatalf("missing output %q: %s", tt.message, output.String())
			}
			if (tt.name == "help" || tt.name == "explicit address bypasses config") && cfgCalls != 0 {
				t.Fatal("unexpected config access")
			}
		})
	}
}

func upgradeArgs(t *testing.T, out *bytes.Buffer, shutdown func(context.Context) error, restarted bool) int {
	t.Helper()
	return upgradeRestartCommand(nil, out,
		func() (string, error) { return "4242", nil },
		func() (string, error) { return "/fake/bin/cercano", nil },
		shutdown,
		func(context.Context, string, netip.AddrPort) (bool, error) { return restarted, nil })
}

func TestUpgradeRestartShutsDownModelHost(t *testing.T) {
	opts := testHostOptions(t, time.Minute)
	done := startHost(t, context.Background(), opts)
	waitRecord(t, opts.RunDir, 3*time.Second)
	var out bytes.Buffer
	if code := upgradeArgs(t, &out, shutdownModelHostIn(opts.RunDir), true); code != 0 {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
	if err := waitDone(t, done, 5*time.Second); err != nil {
		t.Fatalf("runModelHost: %v", err)
	}
	if _, err := modelhost.ReadRecord(opts.RunDir); err == nil {
		t.Fatal("record still present after shutdown")
	}
}

func TestUpgradeRestartWithoutModelHost(t *testing.T) {
	var out bytes.Buffer
	dir := filepath.Join(t.TempDir(), "run")
	if code := upgradeArgs(t, &out, shutdownModelHostIn(dir), true); code != 0 {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
	if strings.Contains(out.String(), "model host") {
		t.Fatalf("unexpected model host message: %s", out.String())
	}
}

func TestUpgradeRestartModelHostFailureKeepsExitCode(t *testing.T) {
	var out bytes.Buffer
	code := upgradeArgs(t, &out, func(context.Context) error { return errors.New("boom") }, true)
	if code != 0 || !strings.Contains(out.String(), "model host") {
		t.Fatalf("code=%d output=%s", code, out.String())
	}
}

func TestUpgradeRestartShutsDownModelHostWhenNoAgentRunning(t *testing.T) {
	called := 0
	var out bytes.Buffer
	if code := upgradeArgs(t, &out, func(context.Context) error { called++; return nil }, false); code != 0 || called != 1 {
		t.Fatalf("code=%d called=%d", code, called)
	}
}
