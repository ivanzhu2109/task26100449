package replay

import (
	"math/rand"
	"testing"
)

// runBatch 是便捷包装：一次性提交批次并返回查询结果。
func runBatch(records []Record) []QueryResult {
	p := NewProcessor()
	for _, r := range records {
		p.Add(r)
	}
	return p.Run()
}

func add(id, acc int64, dev string) Record {
	return Record{Type: RecordAddEvidence, EvidenceID: id, Account: acc, Device: dev}
}

func revoke(id int64) Record {
	return Record{Type: RecordRevokeEvidence, EvidenceID: id}
}

func query(accounts ...int64) Record {
	return Record{Type: RecordQueryGroup, Accounts: accounts}
}

func checkResult(t *testing.T, got QueryResult, wantSize []int, wantMin []int64) {
	t.Helper()
	if len(got.Size) != len(wantSize) || len(got.MinAccount) != len(wantMin) {
		t.Fatalf("结果长度不符: got %v/%v, want %v/%v", got.Size, got.MinAccount, wantSize, wantMin)
	}
	for i := range wantSize {
		if got.Size[i] != wantSize[i] || got.MinAccount[i] != wantMin[i] {
			t.Fatalf("第 %d 项不符: got (%d,%d), want (%d,%d)",
				i, got.Size[i], got.MinAccount[i], wantSize[i], wantMin[i])
		}
	}
}

// 同一账户与同一设备存在多条独立证据，逐条撤销，只有最后一次才断开。
func TestMultipleEvidenceSamePair(t *testing.T) {
	results := runBatch([]Record{
		add(1, 10, "devA"),
		add(2, 10, "devA"),
		add(3, 20, "devA"),
		query(10, 20), // 10 与 20 通过 devA 同组
		revoke(1),
		query(10, 20), // 证据 2 仍在，关系保持
		revoke(2),
		query(10, 20), // 最后一条断开，10 与 devA 解绑
	})

	checkResult(t, results[0], []int{2, 2}, []int64{10, 10})
	checkResult(t, results[1], []int{2, 2}, []int64{10, 10})
	checkResult(t, results[2], []int{1, 1}, []int64{10, 20})
}

// 带环关联：撤销一条边仍连通；撤销桥接证据后真正拆组。
func TestCycleAndBridge(t *testing.T) {
	results := runBatch([]Record{
		// 环：1-d1-2, 2-d2-3, 3-d3-1
		add(1, 1, "d1"),
		add(2, 2, "d1"),
		add(3, 2, "d2"),
		add(4, 3, "d2"),
		add(5, 3, "d3"),
		add(6, 1, "d3"),
		// 另一组：7-d4-8
		add(7, 7, "d4"),
		add(8, 8, "d4"),
		// 桥：3-d5-7 连接两个组
		add(9, 3, "d5"),
		add(10, 7, "d5"),
		query(1, 2, 3, 7, 8), // 全连通：5 人，最小编号 1
		revoke(6),            // 断开环上一条边，仍连通
		query(1, 2, 3, 7, 8),
		revoke(9),            // 撤销桥的一侧证据
		query(1, 2, 3, 7, 8), // 拆成 {1,2,3} 与 {7,8}
	})

	five := []int{5, 5, 5, 5, 5}
	ones := []int64{1, 1, 1, 1, 1}
	checkResult(t, results[0], five, ones)
	checkResult(t, results[1], five, ones)
	checkResult(t, results[2],
		[]int{3, 3, 3, 2, 2},
		[]int64{1, 1, 1, 7, 7})
}

// 无有效关联的账户自成一组；同一账户重复查询只计一次。
func TestSingletonAndDuplicate(t *testing.T) {
	results := runBatch([]Record{
		add(1, 5, "d1"),
		add(2, 6, "d1"),
		query(5, 5, 6, 99), // 99 从未出现
	})
	checkResult(t, results[0],
		[]int{2, 2, 2, 1},
		[]int64{5, 5, 5, 99})
}

// 后续撤销不能改变此前查询的结果。
func TestHistoryIsImmutable(t *testing.T) {
	results := runBatch([]Record{
		add(1, 1, "d1"),
		add(2, 2, "d1"),
		query(1, 2), // 当时同组
		revoke(1),
		revoke(2),
		query(1, 2), // 现在各自成组
		add(3, 1, "d2"),
		add(4, 2, "d2"),
		query(1, 2), // 重新同组
	})
	checkResult(t, results[0], []int{2, 2}, []int64{1, 1})
	checkResult(t, results[1], []int{1, 1}, []int64{1, 2})
	checkResult(t, results[2], []int{2, 2}, []int64{1, 1})
}

// bruteForce 是独立的暴力参考实现：逐条重演记录，
// 每次查询基于当前有效证据重新建图求连通分量。
func bruteForce(records []Record) []QueryResult {
	type ev struct {
		acc int64
		dev string
	}
	active := make(map[int64]ev)
	var results []QueryResult

	for _, rec := range records {
		switch rec.Type {
		case RecordAddEvidence:
			active[rec.EvidenceID] = ev{rec.Account, rec.Device}
		case RecordRevokeEvidence:
			delete(active, rec.EvidenceID)
		case RecordQueryGroup:
			// 账户 -> 设备集合
			adj := make(map[int64][]string)
			devAcc := make(map[string][]int64)
			for _, e := range active {
				adj[e.acc] = append(adj[e.acc], e.dev)
				devAcc[e.dev] = append(devAcc[e.dev], e.acc)
			}
			comp := make(map[int64]int) // 账户 -> 分量编号
			var compSize []int
			var compMin []int64
			for a := range adj {
				if _, seen := comp[a]; seen {
					continue
				}
				id := len(compSize)
				size := 0
				var minAcc int64
				stack := []int64{a}
				comp[a] = id
				for len(stack) > 0 {
					cur := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					size++
					if minAcc == 0 || cur < minAcc {
						minAcc = cur
					}
					for _, d := range adj[cur] {
						for _, nb := range devAcc[d] {
							if _, seen := comp[nb]; !seen {
								comp[nb] = id
								stack = append(stack, nb)
							}
						}
					}
				}
				compSize = append(compSize, size)
				compMin = append(compMin, minAcc)
			}
			res := QueryResult{
				Size:       make([]int, len(rec.Accounts)),
				MinAccount: make([]int64, len(rec.Accounts)),
			}
			for i, a := range rec.Accounts {
				if id, ok := comp[a]; ok {
					res.Size[i] = compSize[id]
					res.MinAccount[i] = compMin[id]
				} else {
					res.Size[i] = 1
					res.MinAccount[i] = a
				}
			}
			results = append(results, res)
		}
	}
	return results
}

// 随机对拍：每个历史查询的结果必须与暴力参考实现完全一致。
func TestRandomAgainstBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 200; trial++ {
		numAcc := 1 + rng.Intn(12)
		numDev := 1 + rng.Intn(6)
		numRec := 1 + rng.Intn(120)
		var records []Record
		var openIDs []int64
		nextID := int64(1)
		for i := 0; i < numRec; i++ {
			kind := rng.Intn(3)
			if kind == 2 && len(openIDs) == 0 {
				kind = 0
			}
			switch kind {
			case 0:
				records = append(records, add(nextID,
					int64(1+rng.Intn(numAcc)),
					devName(rng.Intn(numDev))))
				openIDs = append(openIDs, nextID)
				nextID++
			case 1:
				records = append(records, query(randomAccounts(rng, numAcc)...))
			case 2:
				k := rng.Intn(len(openIDs))
				records = append(records, revoke(openIDs[k]))
				openIDs = append(openIDs[:k], openIDs[k+1:]...)
			}
		}
		got := runBatch(records)
		want := bruteForce(records)
		if len(got) != len(want) {
			t.Fatalf("trial %d: 查询数不符 got %d want %d", trial, len(got), len(want))
		}
		for qi := range want {
			for k := range want[qi].Size {
				if got[qi].Size[k] != want[qi].Size[k] ||
					got[qi].MinAccount[k] != want[qi].MinAccount[k] {
					t.Fatalf("trial %d 查询 %d 第 %d 项: got (%d,%d) want (%d,%d)",
						trial, qi, k,
						got[qi].Size[k], got[qi].MinAccount[k],
						want[qi].Size[k], want[qi].MinAccount[k])
				}
			}
		}
	}
}

func devName(i int) string {
	return "dev-" + string(rune('a'+i))
}

func randomAccounts(rng *rand.Rand, numAcc int) []int64 {
	k := 1 + rng.Intn(8)
	out := make([]int64, k)
	for i := range out {
		out[i] = int64(1 + rng.Intn(numAcc))
	}
	return out
}
