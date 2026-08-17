// Package prompt renders the embedded prompt templates.
//
// The templates and the company guides are baked into the image rather than
// fetched, so a GitHub outage cannot quietly change how the workers reason.
// Only the per-repository context is live, because that genuinely differs
// between runs.
package prompt

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"

	organizzer "github.com/goodtribes-org/agent/organizzer"
)

// Guides is the baked company context handed to every prompt.
type Guides struct {
	WorkingRoot string
	Invariants  string
	Stacks      string
}

// Data is what the stage templates are rendered with.
type Data struct {
	Guides      Guides
	Repo        string
	Number      int
	Title       string
	Body        string
	Phase       string
	Track       string
	Size        string
	RepoContext string
	Outline     string
}

var (
	once      sync.Once
	guides    Guides
	system    string
	templates map[string]*template.Template
	loadErr   error
)

func load() {
	read := func(name string) string {
		b, err := organizzer.Guides.ReadFile(name)
		if err != nil {
			loadErr = fmt.Errorf("read embedded %s: %w", name, err)
			return ""
		}
		return string(b)
	}
	guides = Guides{
		WorkingRoot: read("guides/working-root.md"),
		Invariants:  read("guides/invariants.md"),
		Stacks:      read("guides/stacks.md"),
	}

	sys, err := organizzer.Prompts.ReadFile("prompts/system.md")
	if err != nil {
		loadErr = fmt.Errorf("read embedded prompts/system.md: %w", err)
		return
	}
	system = string(sys)

	templates = map[string]*template.Template{}
	for _, name := range []string{"request", "plan"} {
		raw, err := organizzer.Prompts.ReadFile("prompts/" + name + ".md")
		if err != nil {
			loadErr = fmt.Errorf("read embedded prompts/%s.md: %w", name, err)
			return
		}
		t, err := template.New(name).Parse(string(raw))
		if err != nil {
			loadErr = fmt.Errorf("parse prompts/%s.md: %w", name, err)
			return
		}
		templates[name] = t
	}
}

// System returns the shared system prompt.
func System() (string, error) {
	once.Do(load)
	return system, loadErr
}

// BakedGuides returns the embedded company context.
func BakedGuides() (Guides, error) {
	once.Do(load)
	return guides, loadErr
}

// Render renders one stage's user prompt. The caller supplies everything
// except the guides, which come from the image.
func Render(stage string, d Data) (string, error) {
	once.Do(load)
	if loadErr != nil {
		return "", loadErr
	}
	t, ok := templates[stage]
	if !ok {
		return "", fmt.Errorf("no prompt template for stage %q", stage)
	}
	d.Guides = guides

	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("render %s prompt: %w", stage, err)
	}
	return buf.String(), nil
}
