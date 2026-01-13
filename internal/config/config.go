package config

// config.go 负责加载与校验 YAML 配置，并提供默认值填充。
import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Log      LogConfig      `yaml:"log"`
	Server   ServerConfig   `yaml:"server"`
	Core     CoreConfig     `yaml:"core"`
	WeCom    WeComConfig    `yaml:"wecom"`
	Unraid   UnraidConfig   `yaml:"unraid"`
	Qinglong QinglongConfig `yaml:"qinglong"`
	Auth     AuthConfig     `yaml:"auth"`
}

type LogConfig struct {
	Level LogLevel `yaml:"level"`
}

type LogLevel string

func (l LogLevel) ToSlogLevel() slog.Level {
	switch strings.ToLower(string(l)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	BaseURL    string `yaml:"base_url"`

	HTTPClientTimeout Duration `yaml:"http_client_timeout"`
	ReadHeaderTimeout Duration `yaml:"read_header_timeout"`
}

type CoreConfig struct {
	StateTTL Duration `yaml:"state_ttl"`
}

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration: 仅支持标量值")
	}
	s := strings.TrimSpace(value.Value)
	if s == "" {
		*d = 0
		return nil
	}
	dd, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration: %w", err)
	}
	*d = Duration(dd)
	return nil
}

func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}

type WeComConfig struct {
	CorpID         string `yaml:"corpid"`
	AgentID        int    `yaml:"agentid"`
	Secret         string `yaml:"secret"`
	Token          string `yaml:"token"`
	EncodingAESKey string `yaml:"encoding_aes_key"`
	APIBaseURL     string `yaml:"api_base_url"`
}

type UnraidConfig struct {
	Endpoint            string `yaml:"endpoint"`
	APIKey              string `yaml:"api_key"`
	Origin              string `yaml:"origin"`
	ForceUpdateMutation string `yaml:"force_update_mutation"`

	LogsField        string  `yaml:"logs_field"`
	LogsTailArg      *string `yaml:"logs_tail_arg"`
	LogsPayloadField string  `yaml:"logs_payload_field"`

	StatsField  string   `yaml:"stats_field"`
	StatsFields []string `yaml:"stats_fields"`

	ForceUpdateArgName      string   `yaml:"force_update_arg"`
	ForceUpdateArgType      string   `yaml:"force_update_arg_type"`
	ForceUpdateReturnFields []string `yaml:"force_update_return_fields"`
}

type QinglongConfig struct {
	Instances []QinglongInstance `yaml:"instances"`
}

type QinglongInstance struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	BaseURL      string `yaml:"base_url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type AuthConfig struct {
	AllowedUserIDs []string `yaml:"allowed_userids"`
}

var qinglongInstanceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)
var graphqlIdentifierPattern = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Server.ListenAddr == "" {
		cfg.Server.ListenAddr = ":8080"
	}
	if cfg.Server.HTTPClientTimeout == 0 {
		cfg.Server.HTTPClientTimeout = Duration(15 * time.Second)
	}
	if cfg.Server.ReadHeaderTimeout == 0 {
		cfg.Server.ReadHeaderTimeout = Duration(10 * time.Second)
	}
	if cfg.Core.StateTTL == 0 {
		cfg.Core.StateTTL = Duration(30 * time.Minute)
	}
	if cfg.WeCom.APIBaseURL == "" {
		cfg.WeCom.APIBaseURL = "https://qyapi.weixin.qq.com/cgi-bin"
	}
	if cfg.Unraid.Origin == "" {
		cfg.Unraid.Origin = "wecom-home-ops"
	}
	if cfg.Unraid.LogsField == "" {
		cfg.Unraid.LogsField = "logs"
	}
	if cfg.Unraid.LogsTailArg == nil {
		v := "tail"
		cfg.Unraid.LogsTailArg = &v
	}
	if cfg.Unraid.StatsField == "" {
		cfg.Unraid.StatsField = "stats"
	}
	if cfg.Unraid.StatsFields == nil {
		cfg.Unraid.StatsFields = []string{"cpuPercent", "memUsage", "memLimit", "netIO", "blockIO", "pids"}
	}
	if cfg.Unraid.ForceUpdateMutation == "" {
		cfg.Unraid.ForceUpdateMutation = "update"
	}
	if cfg.Unraid.ForceUpdateArgName == "" {
		cfg.Unraid.ForceUpdateArgName = "id"
	}
	if cfg.Unraid.ForceUpdateArgType == "" {
		cfg.Unraid.ForceUpdateArgType = "PrefixedID!"
	}
	if cfg.Unraid.ForceUpdateReturnFields == nil {
		cfg.Unraid.ForceUpdateReturnFields = []string{"__typename"}
	}
}

func validate(cfg Config) error {
	var problems []string

	if cfg.Server.ListenAddr == "" {
		problems = append(problems, "server.listen_addr 不能为空")
	}
	if cfg.Server.HTTPClientTimeout.ToDuration() <= 0 {
		problems = append(problems, "server.http_client_timeout 不能为空且必须为正数（例如 15s）")
	}
	if cfg.Server.ReadHeaderTimeout.ToDuration() <= 0 {
		problems = append(problems, "server.read_header_timeout 不能为空且必须为正数（例如 10s）")
	}
	if cfg.Core.StateTTL.ToDuration() <= 0 {
		problems = append(problems, "core.state_ttl 不能为空且必须为正数（例如 30m）")
	}

	if cfg.WeCom.CorpID == "" {
		problems = append(problems, "wecom.corpid 不能为空")
	}
	if cfg.WeCom.AgentID == 0 {
		problems = append(problems, "wecom.agentid 不能为空")
	}
	if cfg.WeCom.Secret == "" {
		problems = append(problems, "wecom.secret 不能为空")
	}
	if cfg.WeCom.Token == "" {
		problems = append(problems, "wecom.token 不能为空")
	}
	if cfg.WeCom.EncodingAESKey == "" {
		problems = append(problems, "wecom.encoding_aes_key 不能为空")
	}
	if cfg.WeCom.APIBaseURL == "" {
		problems = append(problems, "wecom.api_base_url 不能为空")
	}

	hasUnraid := strings.TrimSpace(cfg.Unraid.Endpoint) != "" || strings.TrimSpace(cfg.Unraid.APIKey) != ""
	if hasUnraid {
		if cfg.Unraid.Endpoint == "" {
			problems = append(problems, "unraid.endpoint 不能为空")
		}
		if cfg.Unraid.APIKey == "" {
			problems = append(problems, "unraid.api_key 不能为空")
		}
		if strings.TrimSpace(cfg.Unraid.LogsField) == "" {
			problems = append(problems, "unraid.logs_field 不能为空")
		} else if !graphqlIdentifierPattern.MatchString(cfg.Unraid.LogsField) {
			problems = append(problems, "unraid.logs_field 不合法（需为 GraphQL identifier）")
		}
		if cfg.Unraid.LogsTailArg != nil && strings.TrimSpace(*cfg.Unraid.LogsTailArg) != "" {
			if !graphqlIdentifierPattern.MatchString(*cfg.Unraid.LogsTailArg) {
				problems = append(problems, "unraid.logs_tail_arg 不合法（需为 GraphQL identifier 或留空禁用）")
			}
		}
		if strings.TrimSpace(cfg.Unraid.LogsPayloadField) != "" && !graphqlIdentifierPattern.MatchString(cfg.Unraid.LogsPayloadField) {
			problems = append(problems, "unraid.logs_payload_field 不合法（需为 GraphQL identifier）")
		}

		if strings.TrimSpace(cfg.Unraid.StatsField) == "" {
			problems = append(problems, "unraid.stats_field 不能为空")
		} else if !graphqlIdentifierPattern.MatchString(cfg.Unraid.StatsField) {
			problems = append(problems, "unraid.stats_field 不合法（需为 GraphQL identifier）")
		}
		for i, f := range cfg.Unraid.StatsFields {
			if strings.TrimSpace(f) == "" {
				problems = append(problems, fmt.Sprintf("unraid.stats_fields[%d] 不能为空", i))
				continue
			}
			if !graphqlIdentifierPattern.MatchString(f) {
				problems = append(problems, fmt.Sprintf("unraid.stats_fields[%d] 不合法（需为 GraphQL identifier）", i))
			}
		}

		if strings.TrimSpace(cfg.Unraid.ForceUpdateMutation) == "" {
			problems = append(problems, "unraid.force_update_mutation 不能为空")
		} else if !graphqlIdentifierPattern.MatchString(cfg.Unraid.ForceUpdateMutation) {
			problems = append(problems, "unraid.force_update_mutation 不合法（需为 GraphQL identifier）")
		}
		if strings.TrimSpace(cfg.Unraid.ForceUpdateArgName) == "" {
			problems = append(problems, "unraid.force_update_arg 不能为空")
		} else if !graphqlIdentifierPattern.MatchString(cfg.Unraid.ForceUpdateArgName) {
			problems = append(problems, "unraid.force_update_arg 不合法（需为 GraphQL identifier）")
		}
		if strings.TrimSpace(cfg.Unraid.ForceUpdateArgType) == "" {
			problems = append(problems, "unraid.force_update_arg_type 不能为空")
		} else if !isGraphQLTypeRef(cfg.Unraid.ForceUpdateArgType) {
			problems = append(problems, "unraid.force_update_arg_type 不合法（示例：PrefixedID!、ID!、[String!]!）")
		}
		for i, f := range cfg.Unraid.ForceUpdateReturnFields {
			if strings.TrimSpace(f) == "" {
				problems = append(problems, fmt.Sprintf("unraid.force_update_return_fields[%d] 不能为空", i))
				continue
			}
			if !graphqlIdentifierPattern.MatchString(f) {
				problems = append(problems, fmt.Sprintf("unraid.force_update_return_fields[%d] 不合法（需为 GraphQL identifier）", i))
			}
		}
	}

	if len(cfg.Qinglong.Instances) > 0 {
		seen := make(map[string]struct{})
		for i, ins := range cfg.Qinglong.Instances {
			prefix := fmt.Sprintf("qinglong.instances[%d].", i)
			if strings.TrimSpace(ins.ID) == "" {
				problems = append(problems, prefix+"id 不能为空")
			} else {
				if !qinglongInstanceIDPattern.MatchString(ins.ID) {
					problems = append(problems, prefix+"id 不合法（仅允许字母数字及 _ -，长度≤32，且首字符为字母数字）")
				}
				if _, ok := seen[ins.ID]; ok {
					problems = append(problems, prefix+"id 重复")
				}
				seen[ins.ID] = struct{}{}
			}
			if strings.TrimSpace(ins.Name) == "" {
				problems = append(problems, prefix+"name 不能为空")
			}
			if strings.TrimSpace(ins.BaseURL) == "" {
				problems = append(problems, prefix+"base_url 不能为空")
			} else {
				u, err := url.Parse(ins.BaseURL)
				if err != nil || u.Scheme == "" || u.Host == "" {
					problems = append(problems, prefix+"base_url 不合法")
				}
			}
			if strings.TrimSpace(ins.ClientID) == "" {
				problems = append(problems, prefix+"client_id 不能为空")
			}
			if strings.TrimSpace(ins.ClientSecret) == "" {
				problems = append(problems, prefix+"client_secret 不能为空")
			}
		}
	}

	if len(cfg.Auth.AllowedUserIDs) == 0 {
		problems = append(problems, "auth.allowed_userids 不能为空（MVP 仅支持白名单）")
	}

	hasQinglong := len(cfg.Qinglong.Instances) > 0
	if !hasUnraid && !hasQinglong {
		problems = append(problems, "至少配置一个后端服务：unraid 或 qinglong.instances")
	}

	if len(problems) > 0 {
		return errors.New(fmt.Sprintf("配置校验失败: %s", strings.Join(problems, "; ")))
	}
	return nil
}

func isGraphQLTypeRef(s string) bool {
	input := strings.TrimSpace(s)
	if input == "" {
		return false
	}
	i := 0

	var parseName func() bool
	parseName = func() bool {
		if i >= len(input) {
			return false
		}
		ch := input[i]
		if !(ch == '_' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')) {
			return false
		}
		i++
		for i < len(input) {
			ch = input[i]
			if ch == '_' || (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				i++
				continue
			}
			break
		}
		return true
	}

	var parseType func() bool
	parseType = func() bool {
		if i >= len(input) {
			return false
		}
		if input[i] == '[' {
			i++
			if !parseType() {
				return false
			}
			if i >= len(input) || input[i] != ']' {
				return false
			}
			i++
		} else {
			if !parseName() {
				return false
			}
		}
		if i < len(input) && input[i] == '!' {
			i++
		}
		return true
	}

	if !parseType() {
		return false
	}
	return i == len(input)
}
