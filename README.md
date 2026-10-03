# devicelink — 本地设备关联回放库

供上层身份筛查流程调用的 Go 库：按业务顺序回放"新增设备绑定证据 /
撤销证据 / 查询关联组"三类记录，账户清单、行为记录与查询结果全部保存在
内存中，无接口层、无外部依赖。

## 业务语义

- 每条新增证据有独立编号，关联一个账户与一个设备指纹（按完全相等判断）。
  同一账户与同一设备之间可存在多条独立证据；撤销只移除指定编号那一条，
  只要还有其他有效证据，该账户与设备的关系继续存在。
- 共享至少一个有效设备指纹的账户属于同一关联组，关联具有传递性。
- 每条查询的结果完全由该查询记录之前仍有效的证据决定：后续撤销不改变
  此前查询的结果，后续新增也不提前影响此前查询。
- 查询返回每个账户所在组的不同账户数量，以及组内最小账户编号（组标识）。
  无有效关联的账户自成一组（人数 1，组标识为自身）。

## 使用方式

```go
p := replay.NewProcessor()
p.Add(replay.Record{Type: replay.RecordAddEvidence, EvidenceID: 1, Account: 10, Device: "fp-aaa"})
p.Add(replay.Record{Type: replay.RecordAddEvidence, EvidenceID: 2, Account: 20, Device: "fp-aaa"})
p.Add(replay.Record{Type: replay.RecordQueryGroup, Accounts: []int64{10, 20, 30}})
p.Add(replay.Record{Type: replay.RecordRevokeEvidence, EvidenceID: 1})
p.Add(replay.Record{Type: replay.RecordQueryGroup, Accounts: []int64{10, 20}})

results := p.Run() // 与批次中的查询记录按顺序一一对应
// results[0]: 10 与 20 同组（2 人，组标识 10），30 自成一组
// results[1]: 证据 1 撤销后，10 自成一组，20 仍连着 fp-aaa 但组内只有自己
```

输入为完整的有序记录批次，全部 `Add` 完毕后调用一次 `Run()`；
库假设输入已按业务顺序排列且合法（如撤销的编号一定存在）。

## 算法

离线动态连通性：每条证据的有效期是区间 `[新增位置, 撤销位置)`，
把区间挂到记录时间轴的线段树上（每条边 O(log n) 个节点），
深度优先遍历线段树，进入节点时用**可回滚并查集**合并边、离开时撤销，
在叶子处回答该位置的查询。复杂度 O((E log E + Σ查询项) · α(n))，
反复拆分/合并大组不会退化。

## 测试

```sh
go test ./...          # 全部正确性与规模测试
go test -run TestScaleRepeatedSplitMerge -v ./replay/   # 查看耗时与内存
```

覆盖内容：

- 同一绑定存在多条证据，逐条撤销时只有最后一次才断开关系；
- 带环关联撤销一条边仍连通、撤销桥接证据后真正拆组（人数与最小编号均校验）；
- 历史查询不可变：200 组随机批次与逐条重演的暴力参考实现逐查询对拍；
- 规模测试：10 万账户、约 10 万设备、约 29.2 万条记录、100 万查询项，
  两个 5 万账户大组之间反复经唯一桥接关系合并/拆分 2.2 万次。

本机（Apple Silicon）实测：`Run` 耗时约 0.12 秒（上限 15 秒），
进程峰值内存约 160 MiB（上限 512 MiB，含输入与输出）。
