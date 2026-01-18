package config

// config.go 负责加载与校验 YAML 配置，并提供默认值填充。
import (
	"crypto/sha256"
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
	PVE      PVEConfig      `yaml:"pve"`
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
	// TemplateCardMode 控制模板卡片(template_card)的发送与文本兜底策略：
	// - template_card：仅发送模板卡片（默认）
	// - both：发送模板卡片 + 发送文本菜单兜底（并支持“回复序号”触发同等 EventKey）
	// - text：仅发送文本菜单兜底（并支持“回复序号”触发同等 EventKey）
	//
	// 官方限制（SSOT：https://developer.work.weixin.qq.com/document/path/90236）：
	// - 文本通知/图文展示/按钮交互型：企业微信 3.1.6+ 支持
	// - 微工作台（原企业号）不支持展示模板卡片消息
	TemplateCardMode string `yaml:"template_card_mode"`
}

type UnraidConfig struct {
	Endpoint            string `yaml:"endpoint"`
	APIKey              string `yaml:"api_key"`
	Origin              string `yaml:"origin"`
	ForceUpdateMutation string `yaml:"force_update_mutation"`

	// WebGUI 兜底（用于 GraphQL 不支持/不适合的操作，例如容器更新/重启）。
	// - command_url 默认可由 endpoint 推导（/graphql → /webGui/include/StartCommand.php）
	// - events_url 默认可由 endpoint 推导（/graphql → /plugins/dynamix.docker.manager/include/Events.php）
	WebGUICommandURL string `yaml:"webgui_command_url"`
	WebGUIEventsURL  string `yaml:"webgui_events_url"`
	WebGUICSRFToken  string `yaml:"webgui_csrf_token"`
	WebGUICookie     string `yaml:"webgui_cookie"`

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

type PVEConfig struct {
	Instances []PVEInstance  `yaml:"instances"`
	Alert     PVEAlertConfig `yaml:"alert"`
}

type PVEInstance struct {
	ID                 string `yaml:"id"`
	Name               string `yaml:"name"`
	BaseURL            string `yaml:"base_url"`
	APIToken           string `yaml:"api_token"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type PVEAlertConfig struct {
	Enabled *bool `yaml:"enabled"`

	// Interval 为告警轮询间隔（建议 1m~5m）。
	Interval Duration `yaml:"interval"`
	// Cooldown 为同类告警的冷却时间（避免重复刷屏）。
	Cooldown Duration `yaml:"cooldown"`
	// MuteFor 为“静默告警”默认持续时间（通过企业微信菜单触发）。
	MuteFor Duration `yaml:"mute_for"`

	CPUUsageThreshold     float64 `yaml:"cpu_usage_threshold"`
	MemUsageThreshold     float64 `yaml:"mem_usage_threshold"`
	StorageUsageThreshold float64 `yaml:"storage_usage_threshold"`
}

type AuthConfig struct {
	AllowedUserIDs []string `yaml:"allowed_userids"`
}

var qinglongInstanceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)
var pveInstanceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)
var graphqlIdentifierPattern = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

func Load(path string) (Config, error) {
	fields := []any{
		"path", path,
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		fields = append(fields, "cwd", wd)
	}
	if fi, err := os.Stat(path); err == nil {
		fields = append(fields,
			"file_size", fi.Size(),
			"file_mod_time", fi.ModTime().Format(time.RFC3339),
		)
	}
	slog.Info("读取配置文件", fields...)

	b, err := os.ReadFile(path)
	if err != nil {
		slog.Error("读取配置文件失败", "path", path, "error", err)
		return Config{}, err
	}
	sum := sha256.Sum256(b)
	slog.Info("配置文件读取成功",
		"path", path,
		"file_bytes", len(b),
		"file_sha256", fmt.Sprintf("%x", sum[:]),
	)

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		slog.Error("解析配置文件失败（YAML）", "path", path, "error", err)
		return Config{}, err
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		slog.Error("配置校验失败", "path", path, "error", err)
		return Config{}, err
	}

	slog.Info("配置加载成功（敏感字段已脱敏）",
		"server.listen_addr", cfg.Server.ListenAddr,
		"server.base_url_set", strings.TrimSpace(cfg.Server.BaseURL) != "",
		"server.http_client_timeout", cfg.Server.HTTPClientTimeout.ToDuration().String(),
		"server.read_header_timeout", cfg.Server.ReadHeaderTimeout.ToDuration().String(),
		"core.state_ttl", cfg.Core.StateTTL.ToDuration().String(),
		"log.level", string(cfg.Log.Level),

		"wecom.corpid", maskSensitive(cfg.WeCom.CorpID),
		"wecom.agentid", cfg.WeCom.AgentID,
		"wecom.api_base_url", cfg.WeCom.APIBaseURL,
		"wecom.template_card_mode", cfg.WeCom.TemplateCardMode,
		"wecom.token_len", len(cfg.WeCom.Token),
		"wecom.encoding_aes_key_len", len(cfg.WeCom.EncodingAESKey),
		"wecom.secret_len", len(cfg.WeCom.Secret),

		"auth.allowed_userids_count", len(cfg.Auth.AllowedUserIDs),
		"auth.allowed_userids_sample", maskSensitiveSlice(cfg.Auth.AllowedUserIDs, 3),

		"unraid.enabled", strings.TrimSpace(cfg.Unraid.Endpoint) != "" && strings.TrimSpace(cfg.Unraid.APIKey) != "",
		"qinglong.instances_count", len(cfg.Qinglong.Instances),
		"pve.instances_count", len(cfg.PVE.Instances),
		"pve.enabled", len(cfg.PVE.Instances) > 0,
		"pve.alert_enabled", len(cfg.PVE.Instances) > 0 && cfg.PVE.Alert.Enabled != nil && *cfg.PVE.Alert.Enabled,
	)

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
	if strings.TrimSpace(cfg.WeCom.TemplateCardMode) == "" {
		cfg.WeCom.TemplateCardMode = "template_card"
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
		cfg.Unraid.ForceUpdateMutation = "updateContainer"
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

	if cfg.PVE.Alert.Enabled == nil {
		v := true
		cfg.PVE.Alert.Enabled = &v
	}
	if cfg.PVE.Alert.Interval == 0 {
		cfg.PVE.Alert.Interval = Duration(2 * time.Minute)
	}
	if cfg.PVE.Alert.Cooldown == 0 {
		cfg.PVE.Alert.Cooldown = Duration(10 * time.Minute)
	}
	if cfg.PVE.Alert.MuteFor == 0 {
		cfg.PVE.Alert.MuteFor = Duration(30 * time.Minute)
	}
	if cfg.PVE.Alert.CPUUsageThreshold == 0 {
		cfg.PVE.Alert.CPUUsageThreshold = 90
	}
	if cfg.PVE.Alert.MemUsageThreshold == 0 {
		cfg.PVE.Alert.MemUsageThreshold = 90
	}
	if cfg.PVE.Alert.StorageUsageThreshold == 0 {
		cfg.PVE.Alert.StorageUsageThreshold = 90
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
	switch strings.ToLower(strings.TrimSpace(cfg.WeCom.TemplateCardMode)) {
	case "template_card", "both", "text":
	default:
		problems = append(problems, "wecom.template_card_mode 不合法（仅支持 template_card/both/text）")
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

		hasWebGUIFallback := strings.TrimSpace(cfg.Unraid.WebGUICSRFToken) != "" ||
			strings.TrimSpace(cfg.Unraid.WebGUICookie) != "" ||
			strings.TrimSpace(cfg.Unraid.WebGUICommandURL) != "" ||
			strings.TrimSpace(cfg.Unraid.WebGUIEventsURL) != ""
		if hasWebGUIFallback {
			if strings.TrimSpace(cfg.Unraid.WebGUICSRFToken) == "" {
				problems = append(problems, "unraid.webgui_csrf_token 不能为空（启用 WebGUI 兜底时必填）")
			}
			if strings.TrimSpace(cfg.Unraid.WebGUICommandURL) != "" {
				u, err := url.Parse(cfg.Unraid.WebGUICommandURL)
				if err != nil || u.Scheme == "" || u.Host == "" {
					problems = append(problems, "unraid.webgui_command_url 不合法（示例：http://<ip>/webGui/include/StartCommand.php）")
				}
			}
			if strings.TrimSpace(cfg.Unraid.WebGUIEventsURL) != "" {
				u, err := url.Parse(cfg.Unraid.WebGUIEventsURL)
				if err != nil || u.Scheme == "" || u.Host == "" {
					problems = append(problems, "unraid.webgui_events_url 不合法（示例：http://<ip>/plugins/dynamix.docker.manager/include/Events.php）")
				}
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

	if len(cfg.PVE.Instances) > 0 {
		seen := make(map[string]struct{})
		for i, ins := range cfg.PVE.Instances {
			prefix := fmt.Sprintf("pve.instances[%d].", i)
			if strings.TrimSpace(ins.ID) == "" {
				problems = append(problems, prefix+"id 不能为空")
			} else {
				if !pveInstanceIDPattern.MatchString(ins.ID) {
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
					problems = append(problems, prefix+"base_url 不合法（示例：https://pve.example:8006）")
				}
			}
			if strings.TrimSpace(ins.APIToken) == "" {
				problems = append(problems, prefix+"api_token 不能为空")
			}
		}

		if cfg.PVE.Alert.Enabled == nil {
			problems = append(problems, "pve.alert.enabled 缺失（请设为 true/false）")
		} else if *cfg.PVE.Alert.Enabled {
			if cfg.PVE.Alert.Interval.ToDuration() <= 0 {
				problems = append(problems, "pve.alert.interval 不能为空且必须为正数（例如 2m）")
			}
			if cfg.PVE.Alert.Cooldown.ToDuration() <= 0 {
				problems = append(problems, "pve.alert.cooldown 不能为空且必须为正数（例如 10m）")
			}
			if cfg.PVE.Alert.MuteFor.ToDuration() <= 0 {
				problems = append(problems, "pve.alert.mute_for 不能为空且必须为正数（例如 30m）")
			}
			if cfg.PVE.Alert.CPUUsageThreshold <= 0 || cfg.PVE.Alert.CPUUsageThreshold > 100 {
				problems = append(problems, "pve.alert.cpu_usage_threshold 不合法（范围 1~100）")
			}
			if cfg.PVE.Alert.MemUsageThreshold <= 0 || cfg.PVE.Alert.MemUsageThreshold > 100 {
				problems = append(problems, "pve.alert.mem_usage_threshold 不合法（范围 1~100）")
			}
			if cfg.PVE.Alert.StorageUsageThreshold <= 0 || cfg.PVE.Alert.StorageUsageThreshold > 100 {
				problems = append(problems, "pve.alert.storage_usage_threshold 不合法（范围 1~100）")
			}
		}
	}

	if len(cfg.Auth.AllowedUserIDs) == 0 {
		problems = append(problems, "auth.allowed_userids 不能为空（MVP 仅支持白名单）")
	}

	hasQinglong := len(cfg.Qinglong.Instances) > 0
	hasPVE := len(cfg.PVE.Instances) > 0
	if !hasUnraid && !hasQinglong && !hasPVE {
		problems = append(problems, "至少配置一个后端服务：unraid 或 qinglong.instances 或 pve.instances")
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

func maskSensitive(s string) string {
	input := strings.TrimSpace(s)
	if input == "" {
		return ""
	}
	if len(input) <= 2 {
		return "**"
	}
	if len(input) <= 8 {
		return input[:1] + strings.Repeat("*", len(input)-2) + input[len(input)-1:]
	}
	return input[:3] + strings.Repeat("*", len(input)-6) + input[len(input)-3:]
}

func maskSensitiveSlice(ss []string, max int) []string {
	if len(ss) == 0 || max <= 0 {
		return nil
	}
	n := len(ss)
	if n > max {
		n = max
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, maskSensitive(ss[i]))
	}
	return out
}
