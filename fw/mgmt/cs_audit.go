package mgmt

import (
	"fmt"
	"time"

	"github.com/named-data/ndnd/fw/core"
	"github.com/named-data/ndnd/fw/table"
	enc "github.com/named-data/ndnd/std/encoding"
)

// CsAuditModule 提供本机（/localhost）上的缓存审计查询接口。
//
// 中文说明：
// - 本模块不直接读取转发线程的 CS，而是查询 table 包里由审计者 goroutine 维护的 CSNAT（前缀聚合树）。
// - 目前叶子标签为 BLSTag(Name, Wire)（占位实现：HMAC-SHA256），聚合值为 SHA-256 Merkle 化聚合。
//
// 接口约定：
// - /localhost/nfd/cs-audit/agg[/<prefix...>]  -> 返回 prefix 子树的聚合 tag（32 bytes）
// - /localhost/nfd/cs-audit/leaf/<name...>    -> 返回精确 name 的叶子 tag（32 bytes）
// - /localhost/nfd/cs-audit/flip/<name...>    -> 对指定 name 的缓存条目进行随机 1-bit 翻转（用于验证审计）
type CsAuditModule struct {
	manager *Thread
}

func (m *CsAuditModule) String() string { return "mgmt-cs-audit" }
func (m *CsAuditModule) registerManager(manager *Thread) {
	m.manager = manager
}
func (m *CsAuditModule) getManager() *Thread { return m.manager }

func (m *CsAuditModule) handleIncomingInterest(interest *Interest) {
	// Only allow from /localhost
	if !LOCAL_PREFIX.IsPrefix(interest.Name()) {
		core.Log.Warn(m, "Received CS audit Interest from non-local source")
		return
	}

	if len(interest.Name()) < len(LOCAL_PREFIX)+2 {
		core.Log.Warn(m, "Malformed CS audit Interest", "name", interest.Name())
		m.manager.sendCtrlResp(interest, 400, "Bad request", nil)
		return
	}

	verb := interest.Name()[len(LOCAL_PREFIX)+1].String()
	switch verb {
	case "agg":
		m.agg(interest)
	case "leaf":
		m.leaf(interest)
	case "flip":
		m.flip(interest)
	default:
		core.Log.Warn(m, "Received Interest for non-existent verb", "verb", verb)
		m.manager.sendCtrlResp(interest, 501, "Unknown verb", nil)
	}
}

func (m *CsAuditModule) agg(interest *Interest) {
	// 解析 prefix：/localhost/nfd/cs-audit/agg[/<prefix...>]
	var prefix enc.Name
	if len(interest.Name()) > len(LOCAL_PREFIX)+2 {
		prefix = interest.Name()[len(LOCAL_PREFIX)+2:]
	} else {
		prefix = enc.Name{}
	}
	// 中文说明：nfdc 会在末尾追加 "_"（避免 status dataset WithVersion 覆盖 prefix 末尾的版本组件）。
	if len(prefix) > 0 && prefix.At(-1).IsGeneric("_") {
		prefix = prefix.Prefix(-1)
	}

	sum, ok := table.GetCsNatSha256Agg(prefix)
	if !ok {
		m.manager.sendCtrlResp(interest, 404, "Prefix not found", nil)
		return
	}

	// 内容是 32 字节聚合 tag（SHA-256 Merkle 化聚合值）
	buf := make([]byte, 32)
	copy(buf, sum[:])

	// 使用 status dataset 返回（与 cs-info 一致，方便工具复用 object client）
	name := LOCAL_PREFIX.
		Append(enc.NewGenericComponent("cs-audit")).
		Append(enc.NewGenericComponent("agg")).
		Append(prefix...).
		// 中文说明：追加 "_"，保证 dataset 的版本组件不会覆盖 prefix 自带的版本组件。
		Append(enc.NewGenericComponent("_"))
	m.manager.sendStatusDataset(interest, name, enc.Wire{buf})
}

func (m *CsAuditModule) flip(interest *Interest) {
	// 解析 name：/localhost/nfd/cs-audit/flip/<name...>
	if len(interest.Name()) <= len(LOCAL_PREFIX)+2 {
		core.Log.Warn(m, "Missing flip target name", "name", interest.Name())
		m.manager.sendCtrlResp(interest, 400, "Missing flip target name", nil)
		return
	}
	targetWithMarker := interest.Name()[len(LOCAL_PREFIX)+2:]
	target := targetWithMarker
	// 中文说明：为了避免 status dataset 生成时的 WithVersion() 把“目标 name 的版本组件”误当作 dataset 版本而覆盖，
	// nfdc 会在请求名末尾追加一个 "_" 组件。这里需要在真正查 CS 时把它去掉。
	if len(target) > 0 && target.At(-1).IsGeneric("_") {
		target = target.Prefix(-1)
	}

	// 中文说明：等待一次转发线程处理结果（超时则仅表示“已提交但未确认”）。
	res, queued := table.RequestCsAuditFlip(target, 800*time.Millisecond)
	if !queued {
		m.manager.sendCtrlResp(interest, 503, "Flip queue full", nil)
		return
	}

	msg := "queued=1 flipped=0 found=0"
	if res.Time.IsZero() {
		// 超时：没拿到转发线程返回（但请求已入队）
		msg = "queued=1 flipped=0 found=unknown timeout=1"
	} else {
		msg = fmt.Sprintf("queued=1 flipped=%t found=%t byteIndex=%d bitIndex=%d old=%d new=%d time=%s",
			res.Flipped,
			res.Found,
			res.ByteIndex,
			res.BitIndex,
			res.OldByte,
			res.NewByte,
			res.Time.Format(time.RFC3339Nano),
		)
	}

	name := LOCAL_PREFIX.
		Append(enc.NewGenericComponent("cs-audit")).
		Append(enc.NewGenericComponent("flip")).
		Append(target...).
		// 中文说明：无论目标 name 是否以版本组件结尾，都追加一个 "_"，保证 sendStatusDataset 的 WithVersion() 不会覆盖目标版本。
		Append(enc.NewGenericComponent("_"))
	m.manager.sendStatusDataset(interest, name, enc.Wire{[]byte(msg)})
}

func (m *CsAuditModule) leaf(interest *Interest) {
	// 解析 name：/localhost/nfd/cs-audit/leaf/<name...>
	if len(interest.Name()) <= len(LOCAL_PREFIX)+2 {
		core.Log.Warn(m, "Missing leaf name", "name", interest.Name())
		m.manager.sendCtrlResp(interest, 400, "Missing leaf name", nil)
		return
	}
	target := interest.Name()[len(LOCAL_PREFIX)+2:]
	// 中文说明：nfdc 会在末尾追加 "_"（避免 status dataset WithVersion 覆盖目标 name 的版本组件）。
	if len(target) > 0 && target.At(-1).IsGeneric("_") {
		target = target.Prefix(-1)
	}

	sum, ok := table.GetCsNatSha256Leaf(target)
	if !ok {
		m.manager.sendCtrlResp(interest, 404, "Name not found", nil)
		return
	}

	buf := make([]byte, 32)
	copy(buf, sum[:])

	name := LOCAL_PREFIX.
		Append(enc.NewGenericComponent("cs-audit")).
		Append(enc.NewGenericComponent("leaf")).
		Append(target...).
		// 中文说明：追加 "_"，保证 dataset 的版本组件不会覆盖目标 name 自带的版本组件。
		Append(enc.NewGenericComponent("_"))

	m.manager.sendStatusDataset(interest, name, enc.Wire{buf})
}
