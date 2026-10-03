package devlink

// dsuChange 记录一次 union 造成的修改，用于回滚。
type dsuChange struct {
	child     int32 // 被挂接的根
	parentBal int32 // 合并前新根的平衡权重
	parentCnt int32 // 合并前新根的账户数
	parentMin int64 // 合并前新根的最小账户编号
	merged    bool  // false 表示两端已连通，仅为占位，保证快照对齐
}

// rollbackDSU 是可回滚并查集：按平衡权重合并、无路径压缩，
// 维护每个连通块的账户数与最小账户编号。find 复杂度 O(log n)。
type rollbackDSU struct {
	parent []int32
	bal    []int32 // 平衡权重（节点总数），仅用于合并启发式
	cnt    []int32 // 连通块内账户数
	minAcc []int64 // 连通块内最小账户编号
	stack  []dsuChange
}

// newRollbackDSU 构建 n 个元素的并查集。
// weight[i] 为元素 i 的账户权重（账户为 1，设备为 0），
// minAcc[i] 为元素 i 的初始最小编号（设备取极大值，不参与比较）。
func newRollbackDSU(n int, weight []int32, minAcc []int64) *rollbackDSU {
	d := &rollbackDSU{
		parent: make([]int32, n),
		bal:    make([]int32, n),
		cnt:    make([]int32, n),
		minAcc: make([]int64, n),
	}
	for i := 0; i < n; i++ {
		d.parent[i] = int32(i)
		d.bal[i] = 1
		d.cnt[i] = weight[i]
		d.minAcc[i] = minAcc[i]
	}
	return d
}

// find 返回 x 所在集合的根。
func (d *rollbackDSU) find(x int32) int32 {
	for d.parent[x] != x {
		x = d.parent[x]
	}
	return x
}

// union 合并 a、b 所在集合。
func (d *rollbackDSU) union(a, b int32) {
	ra, rb := d.find(a), d.find(b)
	if ra == rb {
		d.stack = append(d.stack, dsuChange{merged: false})
		return
	}
	if d.bal[ra] < d.bal[rb] {
		ra, rb = rb, ra
	}
	d.stack = append(d.stack, dsuChange{
		child:     rb,
		parentBal: d.bal[ra],
		parentCnt: d.cnt[ra],
		parentMin: d.minAcc[ra],
		merged:    true,
	})
	d.parent[rb] = ra
	d.bal[ra] += d.bal[rb]
	d.cnt[ra] += d.cnt[rb]
	if d.minAcc[rb] < d.minAcc[ra] {
		d.minAcc[ra] = d.minAcc[rb]
	}
}

// snapshot 记录当前修改栈高度，供之后回滚。
func (d *rollbackDSU) snapshot() int {
	return len(d.stack)
}

// rollback 撤销到指定快照。
func (d *rollbackDSU) rollback(to int) {
	for len(d.stack) > to {
		c := d.stack[len(d.stack)-1]
		d.stack = d.stack[:len(d.stack)-1]
		if !c.merged {
			continue
		}
		root := d.parent[c.child]
		d.bal[root] = c.parentBal
		d.cnt[root] = c.parentCnt
		d.minAcc[root] = c.parentMin
		d.parent[c.child] = c.child
	}
}

// count 返回 x 所在连通块的账户数。
func (d *rollbackDSU) count(x int32) int {
	return int(d.cnt[d.find(x)])
}

// min 返回 x 所在连通块的最小账户编号。
func (d *rollbackDSU) min(x int32) int64 {
	return d.minAcc[d.find(x)]
}
