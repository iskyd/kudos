package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
)

func TestRenderGolden(t *testing.T) {
	t.Parallel()
	in := PromptInput{Today: date("2026-11-02"), Context: BuildContext(sample(), 0), Schema: `{"type": "object"}`}
	for _, m := range []Mode{ModeChat, ModeDailyInsight, ModeWeeklyReport, ModePlan} {
		p, err := Render(m, in)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if !strings.HasPrefix(p.Version, string(m)+".v") {
			t.Errorf("%s: version %q", m, p.Version)
		}
		golden(t, "prompts/"+p.Version, strings.TrimSpace("=== system\n"+p.System+"\n=== user\n"+p.User)+"\n")
		for _, want := range []string{
			"Today is 2026-11-02.",
			"Cite every item you rely on",
			"This is not medical advice.",
			"see a doctor promptly",
			"<athlete_context>\n" + in.Context + "</athlete_context>",
		} {
			if !strings.Contains(p.System, want) {
				t.Errorf("%s: system prompt lacks %q", m, want)
			}
		}
		if (m == ModeChat) != (p.User == "") {
			t.Errorf("%s: user prompt %q", m, p.User)
		}
	}
	if p, _ := Render(ModePlan, in); !strings.Contains(p.User, in.Schema) {
		t.Errorf("plan prompt lacks the schema:\n%s", p.User)
	}
}

func TestRenderRejects(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mode Mode
		in   PromptInput
	}{
		"unknown mode":        {Mode("poem"), PromptInput{Context: "x"}},
		"plan without schema": {ModePlan, PromptInput{Context: "x"}},
	} {
		if p, err := Render(tc.mode, tc.in); err == nil {
			t.Errorf("%s: rendered %+v", name, p)
		}
	}
}

// Outputs store the version that made them, so a released template must
// never change: write a new version instead, then add its hash here.
func TestReleasedPromptsNeverChange(t *testing.T) {
	t.Parallel()
	released := map[string]string{
		"rules.v1.tmpl":         "c738ae1f134dd80303fd8e8002244c560605024786cce7274055704d5cccdcea",
		"chat.v1.tmpl":          "d06e4926cccd84e221bf44a4784bddbf13e822a1579c9a1253104ea883ccbf79",
		"daily-insight.v1.tmpl": "e32d504ecab9e46322aa1a913645f3224d422837249ea8171fec3e2badcbe2e7",
		"weekly-report.v1.tmpl": "92532c2685bfc2e7707ca5de54dd50df32e86c87543c32b49648c14ed32e5d30",
		"plan.v1.tmpl":          "39916f2b44205222069c269efc8ebd6661589181724e781e1de3370d2db3c43a",
	}
	files, err := fs.Glob(promptFiles, "prompts/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(released) {
		t.Errorf("%d templates, %d hashes: add the new version's hash", len(files), len(released))
	}
	for _, f := range files {
		b, err := fs.ReadFile(promptFiles, f)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got, name := hex.EncodeToString(sum[:]), strings.TrimPrefix(f, "prompts/"); got != released[name] {
			t.Errorf("%s has hash %s: released templates never change, write a new version", name, got)
		}
	}
}
