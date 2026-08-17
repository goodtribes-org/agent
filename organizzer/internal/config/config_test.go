package config

import (
	"strings"
	"testing"
)

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		"GITHUB_TOKEN":   "ghp_test",
		"BERGET_API_KEY": "sk-test",
	}
}

func TestLoadDefaults(t *testing.T) {
	withEnv(t, baseEnv())

	cfg, err := Load(StageRequest)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checks := map[string]struct{ got, want any }{
		"org":          {cfg.GitHubOrg, "goodtribes-org"},
		"project":      {cfg.ProjectNumber, 2},
		"busy poll":    {cfg.PollBusy.String(), "10s"},
		"idle poll":    {cfg.PollIdle.String(), "1m0s"},
		"berget url":   {cfg.BergetBaseURL, "https://api.berget.ai/v1"},
		"webhook path": {cfg.WebhookPath, "/api/v1/tasks"},
		"agent":        {cfg.FoundryAgent, "open-code"},
		"needs-human":  {cfg.NeedsHumanLabel, "needs-human"},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	if len(cfg.AllowedRepos) != 3 {
		t.Errorf("AllowedRepos = %v, want the three goodtribes product repos", cfg.AllowedRepos)
	}
	if len(cfg.PriorityTracks) != 0 {
		t.Errorf("PriorityTracks = %v, want none — no board has a Track field", cfg.PriorityTracks)
	}
}

// A stage waiting on the model is working, not wedged. berget does not stream,
// so a long plan returns nothing for minutes; if the readiness window is
// shorter than the call the loop is allowed to make, a healthy stage reports
// itself unready in the middle of doing exactly what it was configured to do.
func TestReadinessWindowOutlastsTheLongestPermittedModelCall(t *testing.T) {
	env := baseEnv()
	env["BERGET_TIMEOUT_SECONDS"] = "900"
	withEnv(t, env)

	cfg, err := Load(StagePlan)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HealthStaleFor <= cfg.BergetTimeout {
		t.Errorf("HealthStaleFor = %s, must outlast a %s model call",
			cfg.HealthStaleFor, cfg.BergetTimeout)
	}

	// And a short timeout must not drag the window below the idle-poll floor,
	// or a genuinely wedged loop sits unnoticed.
	env["BERGET_TIMEOUT_SECONDS"] = "30"
	withEnv(t, env)
	cfg, err = Load(StagePlan)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := 10 * cfg.PollIdle; cfg.HealthStaleFor != want {
		t.Errorf("HealthStaleFor = %s, want the %s floor", cfg.HealthStaleFor, want)
	}
}

// The request stage produces a short structured verdict and gets the cheap
// model; the plan stage produces the artifact an autonomous agent executes
// verbatim and gets the better one. The apply stage never calls a model.
func TestPerStageModels(t *testing.T) {
	withEnv(t, baseEnv())

	req, err := Load(StageRequest)
	if err != nil {
		t.Fatalf("Load(request): %v", err)
	}
	if !strings.Contains(req.Model, "Mistral-Small") {
		t.Errorf("request model = %q, want the cheap model", req.Model)
	}

	plan, err := Load(StagePlan)
	if err != nil {
		t.Fatalf("Load(plan): %v", err)
	}
	if !strings.Contains(plan.Model, "GLM-5.2") {
		t.Errorf("plan model = %q, want GLM-5.2", plan.Model)
	}
	if plan.MaxTokens <= req.MaxTokens {
		t.Errorf("plan token budget (%d) should exceed the request budget (%d)",
			plan.MaxTokens, req.MaxTokens)
	}

	apply, err := Load(StageApply)
	if err != nil {
		t.Fatalf("Load(apply): %v", err)
	}
	if apply.Model != "" {
		t.Errorf("apply stage has model %q; it must not call one", apply.Model)
	}
}

func TestApplyStageNeedsNoBergetKey(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	if _, err := Load(StageApply); err != nil {
		t.Fatalf("the apply stage should not require a model key: %v", err)
	}
}

func TestMissingCredentialsAreRejectedAtStartup(t *testing.T) {
	if _, err := Load(StageRequest); err == nil {
		t.Fatal("Load accepted a configuration with no GITHUB_TOKEN and no BERGET_API_KEY")
	} else if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error should name the missing variable, got %q", err)
	}
}

func TestDryRunDoesNotRequireCredentialsItWillNotUse(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("DRY_RUN", "true")
	if _, err := Load(StagePlan); err != nil {
		t.Fatalf("a dry run should not require BERGET_API_KEY: %v", err)
	}
}

// A half-pinned status map is worse than none: discovery fills the blanks, so
// one typo'd pin would silently move cards into the wrong column.
func TestPartialStatusPinsAreRejected(t *testing.T) {
	withEnv(t, baseEnv())
	t.Setenv("STATUS_OPTION_NEW", "b863aa86")

	_, err := Load(StageRequest)
	if err == nil {
		t.Fatal("Load accepted a partially pinned status map")
	}
	if !strings.Contains(err.Error(), "all six statuses or none") {
		t.Errorf("error should explain the all-or-nothing rule, got %q", err)
	}
}

func TestFullStatusPinsNeedTheFieldID(t *testing.T) {
	withEnv(t, baseEnv())
	for _, k := range []string{
		"STATUS_OPTION_NEW", "STATUS_OPTION_REQUEST", "STATUS_OPTION_PLAN",
		"STATUS_OPTION_REVIEW", "STATUS_OPTION_APPLY", "STATUS_OPTION_TEST",
	} {
		t.Setenv(k, "opt")
	}

	if _, err := Load(StageRequest); err == nil {
		t.Fatal("Load accepted pinned options with no STATUS_FIELD_ID")
	}

	t.Setenv("STATUS_FIELD_ID", "PVTSSF_test")
	if _, err := Load(StageRequest); err != nil {
		t.Fatalf("Load rejected a fully pinned configuration: %v", err)
	}
}

func TestUnknownStageIsRejected(t *testing.T) {
	withEnv(t, baseEnv())
	if _, err := Load("review"); err == nil {
		t.Fatal("Load accepted a stage that is not one of the three workers")
	}
}

// The operator appends skill env in list order and Kubernetes resolves
// duplicate env names to the last one, so the order the list is written in is
// the order it must arrive in.
func TestFoundrySkillsPreserveOrder(t *testing.T) {
	withEnv(t, baseEnv())
	t.Setenv("FOUNDRY_SKILLS", "berget, berget-large")

	cfg, err := Load(StageApply)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.FoundrySkills) != 2 ||
		cfg.FoundrySkills[0] != "berget" || cfg.FoundrySkills[1] != "berget-large" {
		t.Fatalf("FoundrySkills = %v, want [berget berget-large] in that order", cfg.FoundrySkills)
	}
}

func TestAllowsRepo(t *testing.T) {
	cfg := Config{AllowedRepos: []string{"kickfix", "goodtribes.org"}}
	if !cfg.AllowsRepo("KICKFIX") {
		t.Error("AllowsRepo should be case-insensitive")
	}
	if cfg.AllowsRepo("deploy") {
		t.Error("AllowsRepo accepted a repository not on the list")
	}
}
