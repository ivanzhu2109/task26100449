// Package devlink 提供本地设备关联回放能力：按业务顺序处理一整批
// 新增证据 / 撤销证据 / 查询关联组记录，所有数据保存在内存中。
package devlink

// Kind 表示记录类型。
type Kind int

const (
	// AddEvidence 新增一条设备绑定证据。
	AddEvidence Kind = iota
	// RevokeEvidence 撤销指定编号的证据。
	RevokeEvidence
	// QueryGroup 查询若干账户各自的关联组信息。
	QueryGroup
)

// Record 是一条有序记录，按 Kind 使用对应字段。
type Record struct {
	Kind Kind

	// AddEvidence / RevokeEvidence 使用：证据编号，全局唯一。
	EvidenceID uint64
	// AddEvidence 使用：账户编号（正整数）与设备指纹。
	Account int64
	Device  string

	// QueryGroup 使用：待查询账户列表，至多 500 个。
	Accounts []int64
}

// GroupInfo 是单个账户的关联组查询结果。
type GroupInfo struct {
	// Count 为关联组内不同账户数量。
	Count int
	// MinAccount 为组内最小账户编号，作为组标识。
	MinAccount int64
}

// QueryResult 对应一条 QueryGroup 记录的结果，
// Groups 与查询记录的 Accounts 一一对齐。
type QueryResult struct {
	Groups []GroupInfo
}
