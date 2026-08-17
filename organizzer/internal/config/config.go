// Package config turns the environment into a single validated struct.
//
// Every knob is an environment variable with a working default, so the Helm
// chart only has to set the handful that differ per stage. The two exceptions
// are the credentials, which have no default and are required by the stages
// that use them.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goodtribes-org/agent/organizzer/internal/service"
)

// Stage names. These are also the subcommands and the values of the board's
// Status field that each worker reads.
const (
	StageRequest = "request"
	StagePlan    = "plan"
	StageApply   = "apply"
)

// Config is the whole configuration surface of one worker process.
type Config struct {
	Stage  string
	DryRun bool
	Once   bool

	// GitHub
	GitHubToken   string
	GitHubOrg     string
	GitHubAPIURL  string
	ProjectNumber int

	// Board id pins. Empty means "discover it at startup by name", which is
	// the normal case; these exist so a board edit that breaks discovery can
	// be worked around without a code change.
	ProjectNodeID string
	StatusFieldID string
	StatusOptions map[string]string // status name -> single-select option id

	FieldNameStatus    string
	FieldNamePhase     string
	FieldNameTrack     string
	FieldNameSize      string
	FieldNameBlockedBy string

	AllowedRepos   []string
	PriorityTracks []string

	NeedsHumanLabel string
	NeedsHumanColor string
	ClaimLabel      string

	// Loop
	PollBusy       time.Duration
	PollIdle       time.Duration
	ItemPageSize   int
	BlockerCache   time.Duration
	ItemBackoff    time.Duration
	MaxIssueCmts   int
	MaxIssuesHour  int
	RepoTreeMax    int
	RepoBlobMax    int
	PlanFileFetch  int
	HealthListen   string
	HealthStaleFor time.Duration

	// Berget
	BergetAPIKey  string
	BergetBaseURL string
	BergetTimeout time.Duration
	Model         string
	Temperature   float64
	MaxTokens     int
	JSONMode      bool
	MaxAttempts   int

	// kube-foundry
	WebhookURL     string
	WebhookPath    string
	WebhookSecret  string
	FoundryAgent   string
	FoundrySkills  []string
	FoundrySecret  string
	FoundryBranch  string
	FoundryRetries int
	GitAuthorName  string
	GitAuthorEmail string
	CallbackURL    string
}

// Statuses on the board, in workflow order.
const (
	StatusNew     = "new"
	StatusRequest = "request"
	StatusPlan    = "plan"
	StatusReview  = "review"
	StatusApply   = "apply"
	StatusTest    = "test"
)

// AllStatuses is the set discovery must resolve. A board missing one of these
// is a board this service cannot drive, and startup says so rather than
// failing later on a nil option id.
var AllStatuses = []string{StatusNew, StatusRequest, StatusPlan, StatusReview, StatusApply, StatusTest}

// Load reads the environment for one stage.
func Load(stage string) (Config, error) {
	switch stage {
	case StageRequest, StagePlan, StageApply:
	default:
		return Config{}, fmt.Errorf("unknown stage %q", stage)
	}

	c := Config{
		Stage:  stage,
		DryRun: service.EnvBool("DRY_RUN", false),

		GitHubToken:   strings.TrimSpace(service.Env("GITHUB_TOKEN", "")),
		GitHubOrg:     service.Env("GITHUB_ORG", "goodtribes-org"),
		GitHubAPIURL:  strings.TrimRight(service.Env("GITHUB_API_URL", "https://api.github.com"), "/"),
		// Board 1 is the closed "GoodTribes Project"; 2 is goodtribes.org,
		// 3 kickfix, 4 asylguiden.se. The chart always sets this explicitly.
		ProjectNumber: service.EnvInt("PROJECT_NUMBER", 2),

		ProjectNodeID: service.Env("PROJECT_NODE_ID", ""),
		StatusFieldID: service.Env("STATUS_FIELD_ID", ""),
		StatusOptions: map[string]string{
			StatusNew:     service.Env("STATUS_OPTION_NEW", ""),
			StatusRequest: service.Env("STATUS_OPTION_REQUEST", ""),
			StatusPlan:    service.Env("STATUS_OPTION_PLAN", ""),
			StatusReview:  service.Env("STATUS_OPTION_REVIEW", ""),
			StatusApply:   service.Env("STATUS_OPTION_APPLY", ""),
			StatusTest:    service.Env("STATUS_OPTION_TEST", ""),
		},

		FieldNameStatus:    service.Env("FIELD_NAME_STATUS", "Status"),
		FieldNamePhase:     service.Env("FIELD_NAME_PHASE", "Phase"),
		FieldNameTrack:     service.Env("FIELD_NAME_TRACK", "Track"),
		FieldNameSize:      service.Env("FIELD_NAME_SIZE", "Size"),
		FieldNameBlockedBy: service.Env("FIELD_NAME_BLOCKED_BY", "Blocked by"),

		// The chart narrows this to a single repo per board, so a worker
		// watching the kickfix board cannot be handed an asylguiden card. The
		// default lists all three for the benefit of a local -dry-run.
		AllowedRepos: service.EnvList("ALLOWED_REPOS", []string{
			"goodtribes.org", "kickfix", "asylguiden.se",
		}),
		// Empty: none of the three boards carries a Track field, so ordering
		// falls through to phase, then size, then issue number.
		PriorityTracks: service.EnvList("PRIORITY_TRACKS", nil),

		NeedsHumanLabel: service.Env("NEEDS_HUMAN_LABEL", "needs-human"),
		NeedsHumanColor: service.Env("NEEDS_HUMAN_COLOR", "d4c5f9"),
		ClaimLabel:      service.Env("CLAIM_LABEL", ""),

		PollBusy:      service.EnvSeconds("POLL_BUSY_SECONDS", 10),
		PollIdle:      service.EnvSeconds("POLL_IDLE_SECONDS", 60),
		ItemPageSize:  service.EnvInt("ITEM_PAGE_SIZE", 100),
		BlockerCache:  service.EnvSeconds("BLOCKER_CACHE_SECONDS", 300),
		ItemBackoff:   service.EnvSeconds("ITEM_BACKOFF_SECONDS", 900),
		MaxIssueCmts:  service.EnvInt("MAX_ISSUE_COMMENTS", 50),
		MaxIssuesHour: service.EnvInt("MAX_ISSUES_PER_HOUR", 0), // 0 = no cap
		RepoTreeMax:   service.EnvInt("REPO_TREE_MAX_ENTRIES", 400),
		RepoBlobMax:   service.EnvInt("REPO_BLOB_MAX_BYTES", 32768),
		PlanFileFetch: service.EnvInt("PLAN_FILE_FETCH_MAX", 8),
		HealthListen:  service.Env("HEALTH_LISTEN", ":8080"),

		BergetAPIKey:  strings.TrimSpace(service.Env("BERGET_API_KEY", "")),
		BergetBaseURL: strings.TrimRight(service.Env("BERGET_BASE_URL", "https://api.berget.ai/v1"), "/"),
		// Covers the whole generation, not the time to first byte: berget does
		// not stream, so a plan near its 12000-token budget returns nothing for
		// several minutes. At the observed ~21 tokens/second the old 300s
		// default could not fit a plan over roughly 6300 tokens, and such a
		// card failed identically on every retry.
		BergetTimeout: service.EnvSeconds("BERGET_TIMEOUT_SECONDS", 900),
		Temperature:   0.1,
		JSONMode:      service.EnvBool("LLM_JSON_MODE", false),
		MaxAttempts:   service.EnvInt("LLM_MAX_ATTEMPTS", 3),

		WebhookURL:    strings.TrimRight(service.Env("KUBEFOUNDRY_WEBHOOK_URL", "http://kube-foundry-webhook.kubefoundry.svc.cluster.local"), "/"),
		WebhookPath:   service.Env("KUBEFOUNDRY_WEBHOOK_PATH", "/api/v1/tasks"),
		WebhookSecret: service.Env("KUBEFOUNDRY_WEBHOOK_SECRET", ""),
		// The webhook defaults agent to claude-code and secretRef to
		// factory-creds, neither of which works on this cluster, so both are
		// always sent explicitly.
		FoundryAgent:   service.Env("FOUNDRY_AGENT", "open-code"),
		FoundrySkills:  service.EnvList("FOUNDRY_SKILLS", []string{"berget"}),
		FoundrySecret:  service.Env("FOUNDRY_SECRET_REF", "factory-creds-goodtribes"),
		FoundryBranch:  service.Env("FOUNDRY_BRANCH", "main"),
		FoundryRetries: service.EnvInt("FOUNDRY_MAX_RETRIES", 1),
		GitAuthorName:  service.Env("FOUNDRY_GIT_AUTHOR_NAME", "organizzer"),
		GitAuthorEmail: service.Env("FOUNDRY_GIT_AUTHOR_EMAIL", "organizzer@goodtribes.org"),
		CallbackURL:    service.Env("FOUNDRY_CALLBACK_URL", ""),
	}

	// The readiness probe should fail well before a wedged loop goes unnoticed
	// but comfortably after a normal idle cycle.
	c.HealthStaleFor = 10 * c.PollIdle

	// A cycle that is waiting on the model is working, not wedged. The berget
	// API does not stream, so a plan near its token budget returns nothing at
	// all for minutes — long enough, at the observed rate, to outlast ten idle
	// polls and report a healthy stage as unready. Whatever the loop is
	// configured to wait for has to fit inside the staleness window, with room
	// for the retries around it.
	if floor := c.BergetTimeout + 5*c.PollIdle; floor > c.HealthStaleFor {
		c.HealthStaleFor = floor
	}

	// Per-stage model and token budget. The request stage runs the cheap model
	// because it produces a short structured verdict; the plan stage produces
	// the artifact an autonomous agent executes verbatim and gets the better
	// one. The apply stage never calls a model.
	switch stage {
	case StageRequest:
		c.Model = service.Env("MODEL_REQUEST", "mistralai/Mistral-Small-3.2-24B-Instruct-2506")
		c.MaxTokens = service.EnvInt("LLM_MAX_TOKENS_REQUEST", 4000)
	case StagePlan:
		c.Model = service.Env("MODEL_PLAN", "zai-org/GLM-5.2")
		// GLM-5.2 is a reasoning model and its thinking is charged against this
		// budget — measured at roughly a fifth of the completion on a plan-sized
		// prompt — while the visible plan itself ran to 12000 tokens on the
		// first real card and arrived truncated. The prompt now caps the plan at
		// twelve steps, which is the actual fix; this is the headroom that keeps
		// a legitimately large plan from being thrown away over a few hundred
		// tokens.
		c.MaxTokens = service.EnvInt("LLM_MAX_TOKENS_PLAN", 16000)
	}

	if t := service.Env("LLM_TEMPERATURE", ""); t != "" {
		var f float64
		if _, err := fmt.Sscanf(t, "%g", &f); err == nil {
			c.Temperature = f
		}
	}

	return c, c.Validate()
}

// Validate rejects a configuration that cannot possibly work, at startup
// rather than on the first issue.
func (c Config) Validate() error {
	var errs []error

	if c.GitHubToken == "" {
		errs = append(errs, errors.New("GITHUB_TOKEN is required"))
	}
	if c.GitHubOrg == "" {
		errs = append(errs, errors.New("GITHUB_ORG is required"))
	}
	if c.ProjectNumber <= 0 {
		errs = append(errs, errors.New("PROJECT_NUMBER must be positive"))
	}
	if len(c.AllowedRepos) == 0 {
		errs = append(errs, errors.New("ALLOWED_REPOS must not be empty"))
	}
	if c.PollBusy <= 0 || c.PollIdle <= 0 {
		errs = append(errs, errors.New("POLL_BUSY_SECONDS and POLL_IDLE_SECONDS must be positive"))
	}

	needsLLM := c.Stage == StageRequest || c.Stage == StagePlan
	if needsLLM && !c.DryRun {
		if c.BergetAPIKey == "" {
			errs = append(errs, fmt.Errorf("BERGET_API_KEY is required for the %s stage", c.Stage))
		}
		if c.Model == "" {
			errs = append(errs, fmt.Errorf("no model configured for the %s stage", c.Stage))
		}
	}

	if c.Stage == StageApply && !c.DryRun {
		if c.WebhookURL == "" {
			errs = append(errs, errors.New("KUBEFOUNDRY_WEBHOOK_URL is required for the apply stage"))
		}
		if c.FoundrySecret == "" {
			errs = append(errs, errors.New("FOUNDRY_SECRET_REF is required for the apply stage"))
		}
	}

	// A partially pinned status map is worse than none: discovery fills the
	// blanks, so a typo in one pin would silently move cards to the wrong
	// column. Require all or nothing.
	pinned, blank := 0, 0
	for _, s := range AllStatuses {
		if c.StatusOptions[s] == "" {
			blank++
		} else {
			pinned++
		}
	}
	if pinned > 0 && blank > 0 {
		errs = append(errs, errors.New("STATUS_OPTION_* must be set for all six statuses or none"))
	}
	if pinned == len(AllStatuses) && c.StatusFieldID == "" {
		errs = append(errs, errors.New("STATUS_FIELD_ID is required when the STATUS_OPTION_* pins are set"))
	}

	return errors.Join(errs...)
}

// AllowsRepo reports whether short is one of the repositories this worker is
// permitted to act on.
func (c Config) AllowsRepo(short string) bool {
	for _, r := range c.AllowedRepos {
		if strings.EqualFold(r, short) {
			return true
		}
	}
	return false
}

// IsPriorityTrack reports whether a track sorts ahead of the feature tracks.
func (c Config) IsPriorityTrack(track string) bool {
	for _, t := range c.PriorityTracks {
		if strings.EqualFold(t, track) {
			return true
		}
	}
	return false
}
