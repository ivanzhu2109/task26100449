// Package replay 提供本地设备关联回放能力：按业务顺序处理
// 新增证据、撤销证据与查询关联组三类记录，所有状态保存在内存中。
package replay

// RecordType 标识记录种类。
type RecordType int

const (
	// RecordAddEvidence 新增一条设备绑定证据。
	RecordAddEvidence RecordType = iota
	// RecordRevokeEvidence 撤销指定编号的证据。
	RecordRevokeEvidence
	// RecordQueryGroup 查询一组账户当时的关联组。
	RecordQueryGroup
)

// Record 是一条有序记录，按业务顺序组成完整批次提交。
// 根据 Type 的不同，仅部分字段有效：
//   - RecordAddEvidence:   EvidenceID、Account、Device 有效。
//   - RecordRevokeEvidence: EvidenceID 有效。
//   - RecordQueryGroup:    Accounts 有效（至多 500 个账户）。
type Record struct {
	Type       RecordType
	EvidenceID int64  // 证据独立编号
	Account    int64  // 账户编号（正整数）
	Device     string // 设备指纹，按完全相等判断
	Accounts   []int64
}

// QueryResult 是一条查询记录的结果，与批次中查询记录按顺序一一对应。
type QueryResult struct {
	// Size 与 MinAccount 与对应查询的 Accounts 一一对应：
	// Size 为关联组内不同账户数，MinAccount 为组内最小账户编号（组标识）。
	Size       []int
	MinAccount []int64
}
