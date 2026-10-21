package ui

import (
	"strings"
	"testing"
)

func TestStartupBuildVersion(t *testing.T) {
	for _, version := range []string{"dev", "9.87.654-rc.12+build.123", "v9.87.654"} {
		t.Run(version, func(t *testing.T) {
			m := New(nil, false).WithVersion(version)
			m.width = 160
			want := "v" + strings.TrimPrefix(version, "v")
			for name, output := range map[string]string{"animation": m.splash.View(), "top bar": m.renderHeader()} {
				if !strings.Contains(plain(output), want) || strings.Contains(plain(output), "v0.1.0") {
					t.Errorf("%s missing build version %s: %s", name, want, plain(output))
				}
			}
			m.width = 70
			m.sessionTitle = "session title"
			start, end, ok := m.headerTitleRange()
			if !ok || headerTextWidth(strings.Split(plain(m.renderHeader()), m.sessionTitle)[0]) != start || end-start != headerTextWidth(m.sessionTitle) {
				t.Errorf("header title hit range does not match rendered title: %d:%d", start, end)
			}
		})
	}
}

func TestStartupAnimationOmitsModel(t *testing.T) {
	m := New(nil, false)
	updated, _ := m.Update(configLoadedMsg{OpenModel: "startup-model-sentinel"})
	m = updated.(Model)
	m.width = 160
	if !strings.Contains(plain(m.renderHeader()), "startup-model-sentinel") {
		t.Error("top bar lost model indicator")
	}
	if m.splash.Meta.Model != "" || strings.Contains(plain(m.splash.View()), "startup-model-sentinel") {
		t.Fatalf("startup animation includes model: %+v", m.splash.Meta)
	}
}
