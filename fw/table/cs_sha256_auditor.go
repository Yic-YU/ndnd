package table

import (
	"sync"

	enc "github.com/named-data/ndnd/std/encoding"
)

var csSha256StartOnce sync.Once
var csNatSha256 = newCsNatSha256Tree()

// StartCsSha256Auditor 启动一个同机进程内的“SHA-256 审计者”。
//
// 中文说明：
// - 审计者会持续从 CsAuditEvents 读取事件，对 (Name, Wire) 计算 BLSTag，并写入 CSNAT（前缀聚合树）。
// - 该函数是幂等的：多次调用只会启动一次 goroutine。
func StartCsSha256Auditor() {
	csSha256StartOnce.Do(func() {
		go func() {
			for ev := range CsAuditEvents {
				switch ev.Type {
				case CsAuditEventInsert:
					tag := ComputeCsAuditBlstag(ev.Name, ev.Wire)
					// 中文说明：把 BLSTag(Name, Wire) 当作当前阶段的“审计标签”（tag），写入 CSNAT 的对应叶子节点，
					// 并触发沿路径向上的聚合值重算。
					csNatSha256.OnInsert(ev.Name, tag, ev.StaleTime)
				case CsAuditEventRefresh:
					tag := ComputeCsAuditBlstag(ev.Name, ev.Wire)
					csNatSha256.OnRefresh(ev.Name, tag, ev.StaleTime)
				case CsAuditEventErase:
					// 中文说明：CS 淘汰/删除时，清除叶子标签并尽可能剪枝，保证 CSNAT 与真实 CS 一致。
					_ = csNatSha256.OnErase(ev.Name)
				default:
					// 忽略未知事件
				}
			}
		}()
	})
}

// GetCsNatSha256Agg 查询某个前缀子树的聚合标签（SHA-256）。
func GetCsNatSha256Agg(prefix enc.Name) ([32]byte, bool) {
	return csNatSha256.GetAggregatedTagByPrefix(prefix)
}

// GetCsNatSha256Leaf 查询某个精确 Name 对应的叶子标签（SHA-256）。
func GetCsNatSha256Leaf(name enc.Name) ([32]byte, bool) {
	return csNatSha256.GetLeafTag(name)
}

// GetCsNatSha256Stats 返回 CSNAT 的统计信息（用于审计日志/调试）。
func GetCsNatSha256Stats() (nodeCount uint64, activeLeafCount uint64, rootAgg [32]byte) {
	return csNatSha256.Stats()
}
