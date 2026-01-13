package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	type tmp struct {
		D Duration `yaml:"d"`
	}

	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{
			name: "ok",
			in:   "d: 15s\n",
			want: 15 * time.Second,
		},
		{
			name: "negative-ok-for-unmarshal",
			in:   "d: -1s\n",
			want: -1 * time.Second,
		},
		{
			name: "empty-string",
			in:   "d: \"\"\n",
			want: 0,
		},
		{
			name:    "invalid",
			in:      "d: not-a-duration\n",
			wantErr: true,
		},
		{
			name:    "non-scalar",
			in:      "d: [1]\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got tmp
			err := yaml.Unmarshal([]byte(tc.in), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal() error = nil, want not nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal() error: %v", err)
			}
			if got.D.ToDuration() != tc.want {
				t.Fatalf("duration = %s, want %s", got.D.ToDuration(), tc.want)
			}
		})
	}
}

func TestIsGraphQLTypeRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"PrefixedID!", true},
		{"ID", true},
		{"String!", true},
		{"[String!]", true},
		{"[String!]!", true},
		{"[[String!]!]!", true},
		{"[[[String!]!]!]!", true},
		{"", false},
		{" ", false},
		{"String!!", false},
		{"[String", false},
		{"String]", false},
		{"[String!]]", false},
		{"Bad Type", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(strings.ReplaceAll(tc.in, " ", "_"), func(t *testing.T) {
			t.Parallel()
			if got := isGraphQLTypeRef(tc.in); got != tc.want {
				t.Fatalf("isGraphQLTypeRef(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	t.Parallel()

	var cfg Config
	applyDefaults(&cfg)

	if cfg.Log.Level != "info" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Server.ListenAddr != ":8080" {
		t.Fatalf("Server.ListenAddr = %q, want %q", cfg.Server.ListenAddr, ":8080")
	}
	if cfg.Server.HTTPClientTimeout.ToDuration() != 15*time.Second {
		t.Fatalf("Server.HTTPClientTimeout = %s, want %s", cfg.Server.HTTPClientTimeout.ToDuration(), 15*time.Second)
	}
	if cfg.Server.ReadHeaderTimeout.ToDuration() != 10*time.Second {
		t.Fatalf("Server.ReadHeaderTimeout = %s, want %s", cfg.Server.ReadHeaderTimeout.ToDuration(), 10*time.Second)
	}
	if cfg.Core.StateTTL.ToDuration() != 30*time.Minute {
		t.Fatalf("Core.StateTTL = %s, want %s", cfg.Core.StateTTL.ToDuration(), 30*time.Minute)
	}
	if cfg.WeCom.APIBaseURL != "https://qyapi.weixin.qq.com/cgi-bin" {
		t.Fatalf("WeCom.APIBaseURL = %q, want default", cfg.WeCom.APIBaseURL)
	}
	if cfg.Unraid.Origin != "wecom-home-ops" {
		t.Fatalf("Unraid.Origin = %q, want %q", cfg.Unraid.Origin, "wecom-home-ops")
	}
	if cfg.Unraid.LogsField != "logs" {
		t.Fatalf("Unraid.LogsField = %q, want %q", cfg.Unraid.LogsField, "logs")
	}
	if cfg.Unraid.LogsTailArg == nil || strings.TrimSpace(*cfg.Unraid.LogsTailArg) != "tail" {
		t.Fatalf("Unraid.LogsTailArg = %v, want %q", cfg.Unraid.LogsTailArg, "tail")
	}
	if cfg.Unraid.StatsField != "stats" {
		t.Fatalf("Unraid.StatsField = %q, want %q", cfg.Unraid.StatsField, "stats")
	}
	if len(cfg.Unraid.StatsFields) == 0 {
		t.Fatalf("Unraid.StatsFields empty, want defaults")
	}
	if cfg.Unraid.ForceUpdateMutation != "update" {
		t.Fatalf("Unraid.ForceUpdateMutation = %q, want %q", cfg.Unraid.ForceUpdateMutation, "update")
	}
	if cfg.Unraid.ForceUpdateArgName != "id" {
		t.Fatalf("Unraid.ForceUpdateArgName = %q, want %q", cfg.Unraid.ForceUpdateArgName, "id")
	}
	if cfg.Unraid.ForceUpdateArgType != "PrefixedID!" {
		t.Fatalf("Unraid.ForceUpdateArgType = %q, want %q", cfg.Unraid.ForceUpdateArgType, "PrefixedID!")
	}
	if len(cfg.Unraid.ForceUpdateReturnFields) == 0 {
		t.Fatalf("Unraid.ForceUpdateReturnFields empty, want defaults")
	}
}

func TestValidate_UnraidGraphQLConfig(t *testing.T) {
	t.Parallel()

	invalid := func() Config {
		cfg := Config{
			Server: ServerConfig{
				ListenAddr:        ":8080",
				HTTPClientTimeout: Duration(15 * time.Second),
				ReadHeaderTimeout: Duration(10 * time.Second),
			},
			Core: CoreConfig{
				StateTTL: Duration(30 * time.Minute),
			},
			WeCom: WeComConfig{
				CorpID:         "ww",
				AgentID:        1,
				Secret:         "s",
				Token:          "t",
				EncodingAESKey: "k",
				APIBaseURL:     "https://qyapi.weixin.qq.com/cgi-bin",
			},
			Auth: AuthConfig{
				AllowedUserIDs: []string{"u"},
			},
			Unraid: UnraidConfig{
				Endpoint: "http://x/graphql",
				APIKey:   "k",
			},
		}
		applyDefaults(&cfg)
		return cfg
	}

	cfg := invalid()
	cfg.Unraid.LogsField = "bad-field"
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	tail := "tail!"
	cfg.Unraid.LogsTailArg = &tail
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	cfg.Unraid.StatsField = "stats!"
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	cfg.Unraid.StatsFields = []string{"ok", "bad-field"}
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	cfg.Unraid.ForceUpdateMutation = "update!"
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	cfg.Unraid.ForceUpdateArgType = "PrefixedID!]"
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = invalid()
	cfg.Unraid.ForceUpdateReturnFields = []string{"__typename", "bad-field"}
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}
}

func TestValidate_QinglongInstanceID(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{
			ListenAddr:        ":8080",
			HTTPClientTimeout: Duration(15 * time.Second),
			ReadHeaderTimeout: Duration(10 * time.Second),
		},
		Core: CoreConfig{
			StateTTL: Duration(30 * time.Minute),
		},
		WeCom: WeComConfig{
			CorpID:         "ww",
			AgentID:        1,
			Secret:         "s",
			Token:          "t",
			EncodingAESKey: "k",
			APIBaseURL:     "https://qyapi.weixin.qq.com/cgi-bin",
		},
		Auth: AuthConfig{
			AllowedUserIDs: []string{"u"},
		},
		Qinglong: QinglongConfig{
			Instances: []QinglongInstance{
				{ID: "bad id", Name: "n", BaseURL: "http://x", ClientID: "id", ClientSecret: "sec"},
			},
		},
	}
	applyDefaults(&cfg)
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}

	cfg = Config{
		Server: ServerConfig{
			ListenAddr:        ":8080",
			HTTPClientTimeout: Duration(15 * time.Second),
			ReadHeaderTimeout: Duration(10 * time.Second),
		},
		Core: CoreConfig{
			StateTTL: Duration(30 * time.Minute),
		},
		WeCom: WeComConfig{
			CorpID:         "ww",
			AgentID:        1,
			Secret:         "s",
			Token:          "t",
			EncodingAESKey: "k",
			APIBaseURL:     "https://qyapi.weixin.qq.com/cgi-bin",
		},
		Auth: AuthConfig{
			AllowedUserIDs: []string{"u"},
		},
		Qinglong: QinglongConfig{
			Instances: []QinglongInstance{
				{ID: "home", Name: "n", BaseURL: "http://x", ClientID: "id", ClientSecret: "sec"},
				{ID: "home", Name: "n2", BaseURL: "http://x2", ClientID: "id", ClientSecret: "sec"},
			},
		},
	}
	applyDefaults(&cfg)
	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}
}

func TestValidate_DurationMustBePositive(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{
			ListenAddr:        ":8080",
			HTTPClientTimeout: Duration(-1 * time.Second),
			ReadHeaderTimeout: Duration(10 * time.Second),
		},
		Core: CoreConfig{
			StateTTL: Duration(30 * time.Minute),
		},
		WeCom: WeComConfig{
			CorpID:         "ww",
			AgentID:        1,
			Secret:         "s",
			Token:          "t",
			EncodingAESKey: "k",
			APIBaseURL:     "https://qyapi.weixin.qq.com/cgi-bin",
		},
		Auth: AuthConfig{
			AllowedUserIDs: []string{"u"},
		},
		Qinglong: QinglongConfig{
			Instances: []QinglongInstance{
				{ID: "home", Name: "n", BaseURL: "http://x", ClientID: "id", ClientSecret: "sec"},
			},
		},
	}
	applyDefaults(&cfg)

	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}
}

func TestLoad_SuccessAndDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	in := `
wecom:
  corpid: ww
  agentid: 1
  secret: s
  token: t
  encoding_aes_key: k
auth:
  allowed_userids:
    - u
qinglong:
  instances:
    - id: home
      name: Home
      base_url: http://127.0.0.1:5700
      client_id: id
      client_secret: sec
`
	if err := os.WriteFile(path, []byte(in), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.Server.ListenAddr, ":8080")
	}
	if cfg.Server.HTTPClientTimeout.ToDuration() != 15*time.Second {
		t.Fatalf("HTTPClientTimeout = %s, want %s", cfg.Server.HTTPClientTimeout.ToDuration(), 15*time.Second)
	}
	if cfg.Core.StateTTL.ToDuration() != 30*time.Minute {
		t.Fatalf("StateTTL = %s, want %s", cfg.Core.StateTTL.ToDuration(), 30*time.Minute)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatalf("Load() error = nil, want not nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("wecom: ["), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("Load() error = nil, want not nil")
	}
}

func TestValidate_AtLeastOneBackendRequired(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{
			ListenAddr:        ":8080",
			HTTPClientTimeout: Duration(15 * time.Second),
			ReadHeaderTimeout: Duration(10 * time.Second),
		},
		Core: CoreConfig{
			StateTTL: Duration(30 * time.Minute),
		},
		WeCom: WeComConfig{
			CorpID:         "ww",
			AgentID:        1,
			Secret:         "s",
			Token:          "t",
			EncodingAESKey: "k",
			APIBaseURL:     "https://qyapi.weixin.qq.com/cgi-bin",
		},
		Auth: AuthConfig{
			AllowedUserIDs: []string{"u"},
		},
	}
	applyDefaults(&cfg)

	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}
}

func TestValidate_WeComAndAuthRequiredFields(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server: ServerConfig{
			ListenAddr:        ":8080",
			HTTPClientTimeout: Duration(15 * time.Second),
			ReadHeaderTimeout: Duration(10 * time.Second),
		},
		Core: CoreConfig{
			StateTTL: Duration(30 * time.Minute),
		},
		Qinglong: QinglongConfig{
			Instances: []QinglongInstance{
				{ID: "home", Name: "n", BaseURL: "http://x", ClientID: "id", ClientSecret: "sec"},
			},
		},
	}
	applyDefaults(&cfg)

	if err := validate(cfg); err == nil {
		t.Fatalf("validate() error = nil, want not nil")
	}
}
