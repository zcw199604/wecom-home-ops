// Package pve 封装 Proxmox VE（PVE）API 调用与企业微信交互能力。
package pve

// types.go 定义 PVE API 常用数据结构（按实际使用字段裁剪）。

type VersionInfo struct {
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
	Version string `json:"version"`
}

// ClusterResource 对应 /cluster/resources 返回的条目。
// 说明：该接口会返回多种资源类型（node/storage/qemu/lxc 等），本结构仅保留本项目用到的字段。
type ClusterResource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Node   string `json:"node"`
	Status string `json:"status"`
	Name   string `json:"name"`

	VMID int `json:"vmid"`

	// CPU 为使用率（0~1），PVE 文档标注为 “fraction as percentage”。
	CPU    float64 `json:"cpu"`
	MaxCPU float64 `json:"maxcpu"`

	Mem    int64 `json:"mem"`
	MaxMem int64 `json:"maxmem"`

	Disk    int64 `json:"disk"`
	MaxDisk int64 `json:"maxdisk"`

	Uptime int64 `json:"uptime"`

	// Storage 标识符（type=storage 时可用）
	Storage string `json:"storage"`
}

type TaskStatus struct {
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
	StartTime  int64  `json:"starttime"`
	EndTime    int64  `json:"endtime"`
}

type GuestType string

const (
	GuestTypeQEMU GuestType = "qemu"
	GuestTypeLXC  GuestType = "lxc"
)

func (t GuestType) String() string { return string(t) }

func (t GuestType) IsValid() bool {
	switch t {
	case GuestTypeQEMU, GuestTypeLXC:
		return true
	default:
		return false
	}
}

type GuestAction string

const (
	GuestActionStart    GuestAction = "start"
	GuestActionShutdown GuestAction = "shutdown"
	GuestActionReboot   GuestAction = "reboot"
	GuestActionStop     GuestAction = "stop"
)

func (a GuestAction) String() string { return string(a) }

func (a GuestAction) IsValid() bool {
	switch a {
	case GuestActionStart, GuestActionShutdown, GuestActionReboot, GuestActionStop:
		return true
	default:
		return false
	}
}

