package main

import (
	"os"
	"strings"
	"testing"
)

func TestStableLauncherPrecedesCLIInitialization(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	claim := strings.Index(text, "claimInstallContext(os.Args)")
	launch := strings.Index(text, "stablelaunch.Startup(os.Args[1:], true, verified)")
	flags := strings.Index(text, "handoffFD := flag.Int(")
	config := strings.Index(text, "config.Load(config.DefaultPath())")
	if claim < 0 || launch <= claim || flags <= launch || config <= launch {
		t.Fatal("launcher escaped early verified startup boundary")
	}
}
