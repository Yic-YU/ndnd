package table

import (
	"crypto/rand"
	"time"

	"github.com/named-data/ndnd/fw/core"
	enc "github.com/named-data/ndnd/std/encoding"
)

// 中文说明：这是一个“调试/验证接口”，用于主动制造 CS 缓存条目的比特翻转，
// 以验证审计机制（BLSTag 重算对比）是否能检测并清除被损坏的缓存条目。

type csAuditFlipResult struct {
	Found   bool
	Flipped bool

	ByteIndex int
	BitIndex  int

	OldByte byte
	NewByte byte

	Time time.Time
}

type csAuditFlipReq struct {
	Name  enc.Name
	Reply chan csAuditFlipResult
}

// csAuditFlipReqCh 用于把“翻转请求”从管理线程传递给转发线程处理。
// 注意：转发线程会在 PitCsTree.Update() 中轮询处理，避免跨 goroutine 直接读写 CS。
var csAuditFlipReqCh = make(chan csAuditFlipReq, 16)

// RequestCsAuditFlip 请求对指定 name 的 CS 条目进行 1-bit 随机翻转。
//
// 中文说明：
// - 这是 best-effort：若队列满会返回 ok=false。
// - 若 ok=true 但 timeout 内未收到结果，会返回 ok=true 且 res.Flipped=false（可视为“已提交但未确认”）。
func RequestCsAuditFlip(name enc.Name, timeout time.Duration) (res csAuditFlipResult, ok bool) {
	reply := make(chan csAuditFlipResult, 1)
	req := csAuditFlipReq{
		Name:  name.Clone(),
		Reply: reply,
	}

	select {
	case csAuditFlipReqCh <- req:
		ok = true
	default:
		return res, false
	}

	if timeout <= 0 {
		return res, true
	}

	select {
	case res = <-reply:
		return res, true
	case <-time.After(timeout):
		return res, true
	}
}

func popCsAuditFlipReq() (csAuditFlipReq, bool) {
	select {
	case req := <-csAuditFlipReqCh:
		return req, true
	default:
		return csAuditFlipReq{}, false
	}
}

func (p *PitCsTree) handleCsAuditFlipReq(req csAuditFlipReq) {
	res := csAuditFlipResult{Time: time.Now()}

	// 只支持“精确 name 对应的缓存条目”
	target := req.Name
	node := p.root.findExactMatchEntryEnc(target)
	if (node == nil || node.csEntry == nil) && !target.At(-1).IsSegment() {
		// 中文说明：用户经常只提供到 /.../v=...（未带 seg）。
		// 这里做一个最小的容错：自动尝试 /seg=0。
		target = target.Append(enc.NewSegmentComponent(0))
		node = p.root.findExactMatchEntryEnc(target)
	}
	if node == nil || node.csEntry == nil {
		// 中文说明：也存在“单包对象”的情况：数据名为 /.../v=...，没有 seg 组件。
		// 当用户传入 /.../seg=0 时，这里再反向尝试“去掉 seg”。
		if target.At(-1).IsSegment() {
			target2 := target.Prefix(-1)
			node2 := p.root.findExactMatchEntryEnc(target2)
			if node2 != nil && node2.csEntry != nil {
				target = target2
				node = node2
			}
		}
	}
	if node == nil || node.csEntry == nil || len(node.csEntry.wire) == 0 {
		res.Found = false
		select {
		case req.Reply <- res:
		default:
		}
		return
	}
	res.Found = true

	var r [2]byte
	_, _ = rand.Read(r[:]) // 随机失败也没关系，r 默认全 0
	idx := int(r[0]) % len(node.csEntry.wire)
	bit := int(r[1]) % 8
	mask := byte(1 << uint(bit))

	oldB := node.csEntry.wire[idx]
	newB := oldB ^ mask
	node.csEntry.wire[idx] = newB

	res.Flipped = true
	res.ByteIndex = idx
	res.BitIndex = bit
	res.OldByte = oldB
	res.NewByte = newB

	if CfgCsAuditLogEnabled() {
		core.Log.Info(nil, "【审计】已对缓存条目进行比特翻转（用于验证检测能力）",
			"name", target,
			"byteIndex", idx,
			"bitIndex", bit,
			"old", oldB,
			"new", newB,
		)
	}

	select {
	case req.Reply <- res:
	default:
	}
}
