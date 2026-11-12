package llm

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// promptFiles holds the versioned templates. A released file never changes:
// new wording is a new file, and promptVersion moves to it.
//
//go:embed prompts/*.tmpl
var promptFiles embed.FS

// Mode is what a prompt asks the model to do.
type Mode string

// The prompt modes.
const (
	ModeChat         Mode = "chat"
	ModeDailyInsight Mode = "daily-insight"
	ModeWeeklyReport Mode = "weekly-report"
	ModePlan         Mode = "plan"
	ModeAdjust       Mode = "adjust"
)

// promptVersion names the template each mode uses now.
func promptVersion(m Mode) (string, error) {
	switch m {
	case ModeChat:
		return "chat.v1", nil
	case ModeDailyInsight:
		return "daily-insight.v1", nil
	case ModeWeeklyReport:
		return "weekly-report.v1", nil
	case ModePlan:
		return "plan.v3", nil
	case ModeAdjust:
		return "adjust.v1", nil
	}
	return "", fmt.Errorf("unknown prompt mode %q", m)
}

// PromptInput is what a template fills in.
type PromptInput struct {
	Today   time.Time
	Context string // from BuildContext
	Schema  string // the JSON Schema the reply must match; plan and adjust prompts only
}

// PlanDays is how many days a plan covers, from the day after Today.
const PlanDays = 14

// PlanFrom is the first day a plan covers.
func (in PromptInput) PlanFrom() string { return in.Today.AddDate(0, 0, 1).Format(time.DateOnly) }

// PlanTo is the last day a plan covers.
func (in PromptInput) PlanTo() string { return in.Today.AddDate(0, 0, PlanDays).Format(time.DateOnly) }

// AdjustDays is how many days an adjustment covers, from Today.
const AdjustDays = 7

// AdjustFrom is the first day an adjustment covers.
func (in PromptInput) AdjustFrom() string { return in.Today.Format(time.DateOnly) }

// AdjustTo is the last day an adjustment covers.
func (in PromptInput) AdjustTo() string {
	return in.Today.AddDate(0, 0, AdjustDays-1).Format(time.DateOnly)
}

// Prompt is a rendered template.
type Prompt struct {
	Version string // stored with every output made from this prompt
	System  string
	User    string // the task; empty for chat, where the conversation follows
}

// Render fills in the current template of a mode.
func Render(m Mode, in PromptInput) (Prompt, error) {
	version, err := promptVersion(m)
	if err != nil {
		return Prompt{}, err
	}
	if (m == ModePlan || m == ModeAdjust) && in.Schema == "" {
		return Prompt{}, fmt.Errorf("%s prompt without a schema", m)
	}
	t, err := template.New("").Option("missingkey=error").
		ParseFS(promptFiles, "prompts/rules.*.tmpl", "prompts/"+version+".tmpl")
	if err != nil {
		return Prompt{}, fmt.Errorf("parsing prompt %s: %w", version, err)
	}
	p := Prompt{Version: version}
	if p.System, err = execute(t, "system", in); err != nil {
		return Prompt{}, fmt.Errorf("rendering prompt %s: %w", version, err)
	}
	if t.Lookup("user") != nil {
		if p.User, err = execute(t, "user", in); err != nil {
			return Prompt{}, fmt.Errorf("rendering prompt %s: %w", version, err)
		}
	}
	return p, nil
}

func execute(t *template.Template, name string, in PromptInput) (string, error) {
	var b strings.Builder
	if err := t.ExecuteTemplate(&b, name, in); err != nil {
		return "", fmt.Errorf("executing %s: %w", name, err)
	}
	return strings.TrimSpace(b.String()), nil
}
