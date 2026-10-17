package overlay

import (
	"cercano/source/clients/cli/internal/theme"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Optional render export for visual review; normal tests write no repository files.
func TestCatalogRenderStates(t *testing.T) {
	variants := []struct {
		name  string
		state CatalogState
		query string
	}{{"ready", CatalogState{}, ""}, {"missing-key", CatalogState{Message: "Add an API key to load available models.", ActionLabel: "Add API key", Action: CatalogCredentials}, ""}, {"loading", CatalogState{Message: "Loading available models…", Busy: true}, ""}, {"failure", CatalogState{Message: "Couldn’t load available models. Discovery unavailable", ActionLabel: "Retry", Action: CatalogRefresh}, ""}, {"no-match", CatalogState{}, "nonexistent-model"}, {"partial", CatalogState{Message: "Partial catalog — Personal: discovery failed. Work models remain available."}, ""}}
	for _, v := range variants {
		for _, size := range [][2]int{{120, 36}, {100, 24}, {80, 24}, {60, 18}} {
			for _, th := range theme.BuiltinThemes() {
				c := NewCatalog("Choose a model · Standard quality", "Mistral / Work account", false)
				c.Resize(size[0], size[1])
				c.SetEntries([]CatalogEntry{{Name: "Use inherited recommendation", Action: CatalogInherit}, {ID: "mistral-large-4", Name: "Mistral Large 4", Account: "Work", Details: []CatalogDetail{{Label: "Tools", Value: "Supported"}, {Label: "Vision", Value: "Supported"}, {Label: "Context", Value: "Unknown"}, {Label: "Input / 1M tokens", Value: "Unknown"}, {Label: "Output / 1M tokens", Value: "Unknown"}}}, {ID: "mistral-small-latest", Name: "Mistral Small", Account: "Work", Current: true, Details: []CatalogDetail{{Label: "Tools", Value: "Supported"}, {Label: "Vision", Value: "Unknown"}}}, {ID: "zai-glm-5-3", Name: "GLM 5.3", Account: "Work"}, {Name: "Enter custom model ID…", Action: CatalogCustom}})
				c.State = v.state
				if v.query != "" {
					c.Paste(v.query)
				}
				out := c.View(size[0], size[1], th.Palette)
				if len(strings.Split(out, "\n")) > size[1] {
					t.Fatalf("%s %v %s: height overflow", v.name, size, th.Name)
				}
				for _, line := range strings.Split(out, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("%s %v %s: width overflow", v.name, size, th.Name)
					}
				}
				if dir := os.Getenv("CERCANO_CATALOG_RENDER_DIR"); dir != "" && th.Name == "cr4k3r_j4x" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("%s-%dx%d", v.name, size[0], size[1])
					if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(out)), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(out), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}
