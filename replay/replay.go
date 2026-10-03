package replay

// Processor 处理一个完整的有序记录批次。
// 输入已按业务顺序排列且合法，无须在记录尚未全部交入时输出结果。
//
// 算法：离线动态连通性。每条证据的有效期是 [新增时刻, 撤销时刻)，
// 将区间挂到线段树的 O(log n) 个节点上，深度优先遍历线段树，
// 进入节点时合并边、离开时回滚，叶子处回答该时刻的查询。
// 反复拆分/合并大组也只产生 O((E log E + Σ查询) α) 的工作量。
type Processor struct {
	records []Record
}

// NewProcessor 创建空的批次处理器。
func NewProcessor() *Processor {
	return &Processor{}
}

// Add 追加一条记录，保持业务顺序。
func (p *Processor) Add(r Record) {
	p.records = append(p.records, r)
}

// Run 在全部记录交入后执行整批计算，
// 返回与批次中查询记录一一对应的结果（按查询出现顺序）。
func (p *Processor) Run() []QueryResult {
	n := len(p.records)
	if n == 0 {
		return nil
	}

	// 第一遍：为账户与设备分配节点编号，确定每条证据的有效区间。
	accIndex := make(map[int64]int)
	devIndex := make(map[string]int)
	openAt := make(map[int64]int) // 证据编号 -> 新增时刻

	var edgeAcc, edgeDev []int // 边端点（节点编号）
	var edgeL, edgeR []int     // 有效区间 [l, r)
	addEdge := func(accNode, devNode, l int) {
		edgeAcc = append(edgeAcc, accNode)
		edgeDev = append(edgeDev, devNode)
		edgeL = append(edgeL, l)
		edgeR = append(edgeR, -1) // 占位，撤销或批次结束时确定
	}

	// 查询：按时刻收集。
	type queryAt struct {
		accounts []int64
		resultIx int
	}
	queriesAt := make(map[int][]queryAt)
	var results []QueryResult

	for t, rec := range p.records {
		switch rec.Type {
		case RecordAddEvidence:
			ai, ok := accIndex[rec.Account]
			if !ok {
				ai = len(accIndex)
				accIndex[rec.Account] = ai
			}
			di, ok := devIndex[rec.Device]
			if !ok {
				di = len(devIndex)
				devIndex[rec.Device] = di
			}
			openAt[rec.EvidenceID] = len(edgeAcc)
			addEdge(ai, di, t)
		case RecordRevokeEvidence:
			if ei, ok := openAt[rec.EvidenceID]; ok {
				edgeR[ei] = t
				delete(openAt, rec.EvidenceID)
			}
		case RecordQueryGroup:
			queriesAt[t] = append(queriesAt[t], queryAt{
				accounts: rec.Accounts,
				resultIx: len(results),
			})
			results = append(results, QueryResult{
				Size:       make([]int, len(rec.Accounts)),
				MinAccount: make([]int64, len(rec.Accounts)),
			})
		}
	}
	for _, ei := range openAt {
		edgeR[ei] = n
	}

	numAcc := len(accIndex)
	numNode := numAcc + len(devIndex)
	dsu := newRollbackDSU(numNode)
	for acc, ai := range accIndex {
		dsu.initAccount(ai, acc)
	}
	// 设备节点编号整体偏移到账户之后。
	for i := range edgeDev {
		edgeDev[i] += numAcc
	}

	// 线段树：把每条边的有效区间挂到覆盖节点上。
	seg := make([][]int, 4*max(n, 1))
	var addInterval func(node, lo, hi, l, r, ei int)
	addInterval = func(node, lo, hi, l, r, ei int) {
		if l >= r || r <= lo || hi <= l {
			return
		}
		if l <= lo && hi <= r {
			seg[node] = append(seg[node], ei)
			return
		}
		mid := (lo + hi) / 2
		addInterval(node*2, lo, mid, l, r, ei)
		addInterval(node*2+1, mid, hi, l, r, ei)
	}
	for ei := range edgeAcc {
		addInterval(1, 0, n, edgeL[ei], edgeR[ei], ei)
	}

	// 深度优先遍历，叶子处回答查询。
	var dfs func(node, lo, hi int)
	dfs = func(node, lo, hi int) {
		snap := dsu.snapshot()
		for _, ei := range seg[node] {
			dsu.union(edgeAcc[ei], edgeDev[ei])
		}
		if hi-lo == 1 {
			for _, q := range queriesAt[lo] {
				res := &results[q.resultIx]
				for k, acc := range q.accounts {
					ai, ok := accIndex[acc]
					if !ok {
						// 从未出现过的账户自成一组。
						res.Size[k] = 1
						res.MinAccount[k] = acc
						continue
					}
					res.Size[k] = dsu.accountCount(ai)
					res.MinAccount[k] = dsu.minAccount(ai)
				}
			}
		} else {
			mid := (lo + hi) / 2
			dfs(node*2, lo, mid)
			dfs(node*2+1, mid, hi)
		}
		dsu.rollback(snap)
	}
	dfs(1, 0, n)

	return results
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
