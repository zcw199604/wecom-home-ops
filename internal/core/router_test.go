// Router 相关命令解析单元测试。
package core

import "testing"

func TestParseTextCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		ok        bool
		wantAct   Action
		wantName  string
		wantTail  int
		wantError bool
	}{
		{
			name:     "status",
			input:    "状态 app",
			ok:       true,
			wantAct:  ActionViewStatus,
			wantName: "app",
		},
		{
			name:     "stats",
			input:    "资源 app",
			ok:       true,
			wantAct:  ActionViewStats,
			wantName: "app",
		},
		{
			name:     "detail",
			input:    "详情 app",
			ok:       true,
			wantAct:  ActionViewStatsDetail,
			wantName: "app",
		},
		{
			name:     "logs_default",
			input:    "日志 app",
			ok:       true,
			wantAct:  ActionViewLogs,
			wantName: "app",
			wantTail: defaultLogTail,
		},
		{
			name:     "logs_custom",
			input:    "日志 app 120",
			ok:       true,
			wantAct:  ActionViewLogs,
			wantName: "app",
			wantTail: 120,
		},
		{
			name:      "logs_bad_tail",
			input:     "日志 app xx",
			ok:        true,
			wantError: true,
		},
		{
			name:  "not_command",
			input: "hello",
			ok:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, ok, err := parseTextCommand(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v", ok, tt.ok)
			}
			if tt.wantError {
				if err == nil {
					t.Fatalf("err=nil, want not nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if !tt.ok {
				return
			}
			if cmd.Action != tt.wantAct {
				t.Fatalf("action=%s, want %s", cmd.Action, tt.wantAct)
			}
			if cmd.ContainerName != tt.wantName {
				t.Fatalf("container=%q, want %q", cmd.ContainerName, tt.wantName)
			}
			if cmd.Action == ActionViewLogs && cmd.LogTail != tt.wantTail {
				t.Fatalf("tail=%d, want %d", cmd.LogTail, tt.wantTail)
			}
		})
	}
}
