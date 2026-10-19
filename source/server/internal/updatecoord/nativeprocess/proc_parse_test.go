package nativeprocess

import (
	"strings"
	"testing"
)

func TestProcStartParsing(t *testing.T) {
	suffix := "S " + strings.Repeat("0 ", 18) + "12345 0 0\n"
	for _, comm := range []string{"normal", "a ) b (c)", "space\nand newline"} {
		got, e := parseProcStart([]byte("42 ("+comm+") "+suffix), 42)
		if e != nil || got != 12345 {
			t.Fatal(got, e)
		}
	}
	for _, s := range []string{"", "42 (a) S", "41 (a) " + suffix, "42 a) " + suffix, "42 (a) S " + strings.Repeat("0 ", 18) + "18446744073709551616", "42 (a) S " + strings.Repeat("0 ", 18) + "0"} {
		if _, e := parseProcStart([]byte(s), 42); e == nil {
			t.Fatal("invalid stat accepted", s)
		}
	}
}

func TestProcEffectiveUIDParsing(t *testing.T) {
	got, e := parseProcUID([]byte("Name:\ttest\nUid:\t1\t2\t3\t4\n"))
	if e != nil || got != 2 {
		t.Fatal(got, e)
	}
	for _, s := range []string{"", "Uid: 1 2 3", "Uid: 1 -2 3 4", "Uid: 1 4294967296 3 4", "Uid: 1 2 3 4\nUid: 1 2 3 4"} {
		if _, e := parseProcUID([]byte(s)); e == nil {
			t.Fatal("invalid status accepted", s)
		}
	}
}

func TestNativeIdentityIncludesLinuxLifetime(t *testing.T) {
	first := Identity{PID: 42, UID: 1, Executable: "/bin/a", StartTicks: 123}
	second := first
	second.StartTicks++
	if first.SameProcess(second) {
		t.Fatal("PID reuse accepted")
	}
}
