package replay

// rollbackDSU 是可回滚并查集，用于离线分治回放。
// 节点包含账户与设备；只统计账户节点的人数与最小编号。
// 采用按大小合并、不做路径压缩，配合操作栈实现 O(log n) 回滚。
type rollbackDSU struct {
	parent []int
	size   []int
	// accounts 为连通分量内账户节点数；minAcc 为分量内最小账户编号。
	accounts []int
	minAcc   []int64
	stack    []dsuOp
}

// dsuOp 记录一次合并对根节点的修改，供回滚恢复。
type dsuOp struct {
	x         int // 被挂接的根
	y         int // 合并后的根
	sizeY     int
	accountsY int
	minAccY   int64
}

func newRollbackDSU(n int) *rollbackDSU {
	d := &rollbackDSU{
		parent:   make([]int, n),
		size:     make([]int, n),
		accounts: make([]int, n),
		minAcc:   make([]int64, n),
	}
	for i := range d.parent {
		d.parent[i] = i
		d.size[i] = 1
	}
	return d
}

// initAccount 将节点 x 标记为账户节点，编号为 acc。
func (d *rollbackDSU) initAccount(x int, acc int64) {
	d.accounts[x] = 1
	d.minAcc[x] = acc
}

// find 返回 x 所在分量的根（不做路径压缩，保证可回滚）。
func (d *rollbackDSU) find(x int) int {
	for d.parent[x] != x {
		x = d.parent[x]
	}
	return x
}

// union 合并 x、y 所在分量，返回是否发生合并。
func (d *rollbackDSU) union(x, y int) bool {
	x = d.find(x)
	y = d.find(y)
	if x == y {
		return false
	}
	if d.size[x] > d.size[y] {
		x, y = y, x
	}
	d.stack = append(d.stack, dsuOp{
		x:         x,
		y:         y,
		sizeY:     d.size[y],
		accountsY: d.accounts[y],
		minAccY:   d.minAcc[y],
	})
	d.parent[x] = y
	d.size[y] += d.size[x]
	d.accounts[y] += d.accounts[x]
	if d.accounts[x] > 0 && (d.accounts[y] == d.accounts[x] || d.minAcc[x] < d.minAcc[y]) {
		// y 原本无账户，或 x 的最小编号更小。
		d.minAcc[y] = d.minAcc[x]
	}
	return true
}

// snapshot 记录当前操作栈高度，供回滚使用。
func (d *rollbackDSU) snapshot() int {
	return len(d.stack)
}

// rollback 撤销到指定快照。
func (d *rollbackDSU) rollback(to int) {
	for len(d.stack) > to {
		op := d.stack[len(d.stack)-1]
		d.stack = d.stack[:len(d.stack)-1]
		d.parent[op.x] = op.x
		d.size[op.y] = op.sizeY
		d.accounts[op.y] = op.accountsY
		d.minAcc[op.y] = op.minAccY
	}
}

// accountCount 返回 x 所在分量内的账户数。
func (d *rollbackDSU) accountCount(x int) int {
	return d.accounts[d.find(x)]
}

// minAccount 返回 x 所在分量内的最小账户编号。
func (d *rollbackDSU) minAccount(x int) int64 {
	return d.minAcc[d.find(x)]
}
