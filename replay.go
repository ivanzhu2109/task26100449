package devlink

import "math"

// edge 是账户节点与设备节点之间的一条二分图边。
// 两个账户共享有效设备指纹，当且仅当它们在二分图中连通。
type edge struct {
	u, v int32
}

// interval 是一条边的有效时间范围 [l, r)，取值为记录下标。
type interval struct {
	l, r int
	u, v int32
}

// Replay 按顺序处理完整记录批次，返回每条 QueryGroup 记录对应的结果，
// 顺序与批次中查询记录的出现顺序一致。
//
// 实现思路（离线动态连通性）：
//  1. 第一遍扫描，把每条证据的生命周期换算为记录下标区间：
//     新增于第 a 条、撤销于第 r 条，则对位于 (a, r) 内的查询有效。
//  2. 把每条（账户, 设备）边按有效区间挂到时间轴线段树的 O(log T) 个节点上。
//  3. 深度优先遍历线段树，进入节点时合并该节点的边、离开时回滚；
//     到达叶子即该时刻的有效图，直接回答该位置的查询。
//
// 复杂度 O((E log T + Q) log N)，E 为证据数，Q 为查询项数。
func Replay(records []Record) []QueryResult {
	total := len(records)
	if total == 0 {
		return nil
	}

	// 第一遍：编号压缩 + 计算每条证据边的有效区间。
	accIndex := make(map[int64]int32)
	var accIDs []int64
	accIdxOf := func(a int64) int32 {
		if i, ok := accIndex[a]; ok {
			return i
		}
		i := int32(len(accIDs))
		accIndex[a] = i
		accIDs = append(accIDs, a)
		return i
	}
	devIndex := make(map[string]int32)
	devIdxOf := func(s string) int32 {
		if i, ok := devIndex[s]; ok {
			return i
		}
		i := int32(len(devIndex))
		devIndex[s] = i
		return i
	}

	type openEvidence struct {
		acc, dev int32
		pos      int
	}
	open := make(map[uint64]openEvidence)
	intervals := make([]interval, 0, len(records))

	for i, rec := range records {
		switch rec.Kind {
		case AddEvidence:
			open[rec.EvidenceID] = openEvidence{
				acc: accIdxOf(rec.Account),
				dev: devIdxOf(rec.Device),
				pos: i,
			}
		case RevokeEvidence:
			e, ok := open[rec.EvidenceID]
			if !ok {
				continue
			}
			delete(open, rec.EvidenceID)
			if e.pos+1 < i {
				intervals = append(intervals, interval{l: e.pos + 1, r: i, u: e.acc, v: e.dev})
			}
		case QueryGroup:
			for _, a := range rec.Accounts {
				accIdxOf(a)
			}
		}
	}
	for _, e := range open {
		if e.pos+1 < total {
			intervals = append(intervals, interval{l: e.pos + 1, r: total, u: e.acc, v: e.dev})
		}
	}

	// 第二遍：把边按有效区间挂到时间轴线段树上。
	numAcc := len(accIDs)
	numDev := len(devIndex)
	size := 1
	for size < total {
		size <<= 1
	}
	tree := make([][]edge, 2*size)
	addEdge := func(l, r int, u, v int32) {
		for l, r = l+size, r+size; l < r; l, r = l>>1, r>>1 {
			if l&1 == 1 {
				tree[l] = append(tree[l], edge{u, v})
				l++
			}
			if r&1 == 1 {
				r--
				tree[r] = append(tree[r], edge{u, v})
			}
		}
	}
	for _, iv := range intervals {
		addEdge(iv.l, iv.r, iv.u, int32(numAcc)+iv.v)
	}

	// 并查集：账户节点权重为 1，设备节点权重为 0 且最小编号取极大值。
	weight := make([]int32, numAcc+numDev)
	minAcc := make([]int64, numAcc+numDev)
	for i, id := range accIDs {
		weight[i] = 1
		minAcc[i] = id
	}
	for j := 0; j < numDev; j++ {
		minAcc[numAcc+j] = math.MaxInt64
	}
	dsu := newRollbackDSU(numAcc+numDev, weight, minAcc)

	// 第三遍：DFS 线段树，在叶子处回答查询。
	var results []QueryResult
	var dfs func(node, nl, nr int)
	dfs = func(node, nl, nr int) {
		snap := dsu.snapshot()
		for _, e := range tree[node] {
			dsu.union(e.u, e.v)
		}
		if nr-nl == 1 {
			if nl < total && records[nl].Kind == QueryGroup {
				q := records[nl].Accounts
				res := QueryResult{Groups: make([]GroupInfo, len(q))}
				for k, a := range q {
					idx := accIndex[a]
					res.Groups[k] = GroupInfo{
						Count:      dsu.count(idx),
						MinAccount: dsu.min(idx),
					}
				}
				results = append(results, res)
			}
		} else {
			mid := (nl + nr) >> 1
			dfs(2*node, nl, mid)
			dfs(2*node+1, mid, nr)
		}
		dsu.rollback(snap)
	}
	dfs(1, 0, size)

	return results
}
