package ui

import (
	"fmt"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"github.com/charmbracelet/x/ansi"
)

func TestWizardReviewActionsHaveAlignedColumns(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.width = 80
	wp.state.FlowVersion = 2
	wp.state.Step = wizard.StepDone
	wp.state.LocusMode = "cloud_only"
	wp.curRouting = &agentclient.RoutingAssignments{Tasks: map[string]agentclient.TaskAssignment{"chat": {Destination: "primary", Quality: "standard"}}}
	view := ansi.Strip(wp.View())
	count, col := 0, -1
	for _, line := range strings.Split(view, "\n") {
		i := strings.Index(line, "│")
		if i < 0 {
			continue
		}
		x := ansi.StringWidth(line[:i])
		if col < 0 {
			col = x
		}
		if x != col {
			t.Fatalf("misaligned column: %s", line)
		}
		if ansi.StringWidth(line) > wp.width {
			t.Fatalf("table overflow: %s", line)
		}
		count++
	}
	if count < 3 {
		t.Fatalf("actions lack a name/description table: %s", view)
	}
	if !strings.Contains(view, "Use template task assignments") {
		t.Fatal("long action label was lost")
	}
}

func TestWizardTableFitsNarrowAndWideScreens(t *testing.T) {
	for _, width := range []int{32, 48, 60, 80, 120} {
		for _, step := range []wizard.Step{wizard.StepResources, wizard.StepDone} {
			t.Run(fmt.Sprintf("%d/%s", width, step), func(t *testing.T) {
				wp := newTestWizardPage(t)
				wp.width = width
				wp.cursor = 0
				wp.state.FlowVersion = 2
				wp.state.Step = step
				wp.state.LocusMode = "cloud_only"
				wp.curRouting = &agentclient.RoutingAssignments{Tasks: map[string]agentclient.TaskAssignment{"chat": {Destination: "primary", Quality: "standard"}}}
				wp.curProfiles = []agentclient.CloudProfileInfo{{Name: strings.Repeat("界", 24), Provider: "anthropic", HasKey: true}}
				text := ansi.Strip(wp.renderWizardRows())
				for _, line := range strings.Split(text, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("line exceeds %d columns: %s", width, line)
					}
				}
				if strings.Count(text, "▶ ") != 1 {
					t.Fatal("selection marker missing or repeated across wrapped lines")
				}
				if step == wizard.StepResources && strings.Count(text, "界") != 24 {
					t.Fatal("unicode account name was truncated")
				}
			})
		}
	}
}

func TestWizardCapabilityBulletUsesAccentAndHangingIndent(t *testing.T) {
	wp := newTestWizardPage(t)
	wp.width = 48
	text := wp.renderWizardBullet("Cloud", strings.Repeat("provider capacity ", 8)+"available.")
	if !strings.Contains(text, wp.styles.Accent.Bold(true).Render("Cloud")) {
		t.Fatal("bullet label is not bold accent styled")
	}
	plain := ansi.Strip(text)
	prefix := "  * Cloud - "
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], prefix) {
		t.Fatal("fixture did not produce a formatted wrapping bullet")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > wp.width {
			t.Fatal("bullet exceeds screen width")
		}
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, strings.Repeat(" ", len(prefix))) {
			t.Fatalf("continuation is not aligned with description: %q", line)
		}
	}
}
