package mgmt

import (
	"github.com/named-data/ndnd/fw/core"
	"github.com/named-data/ndnd/fw/table"
	enc "github.com/named-data/ndnd/std/encoding"
)

// CsAuditModule 提供本机（/localhost）上的缓存审计查询接口。
//
// 中文说明：
// - 本模块不直接读取转发线程的 CS，而是查询 table 包里由审计者 goroutine 维护的 CSNAT（前缀聚合树）。
// - 目前返回的是 SHA-256 版本的 tag（未来可升级为 BLS/聚合签名）。
//
// 接口约定：
// - /localhost/nfd/cs-audit/agg[/<prefix...>]  -> 返回 prefix 子树的聚合 tag（32 bytes）
// - /localhost/nfd/cs-audit/leaf/<name...>    -> 返回精确 name 的叶子 tag（32 bytes）
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

	sum, ok := table.GetCsNatSha256Agg(prefix)
	if !ok {
		m.manager.sendCtrlResp(interest, 404, "Prefix not found", nil)
		return
	}

	// 内容是 32 字节 sha256
	buf := make([]byte, 32)
	copy(buf, sum[:])

	// 使用 status dataset 返回（与 cs-info 一致，方便工具复用 object client）
	name := LOCAL_PREFIX.
		Append(enc.NewGenericComponent("cs-audit")).
		Append(enc.NewGenericComponent("agg")).
		Append(prefix...)
	m.manager.sendStatusDataset(interest, name, enc.Wire{buf})
}

func (m *CsAuditModule) leaf(interest *Interest) {
	// 解析 name：/localhost/nfd/cs-audit/leaf/<name...>
	if len(interest.Name()) <= len(LOCAL_PREFIX)+2 {
		core.Log.Warn(m, "Missing leaf name", "name", interest.Name())
		m.manager.sendCtrlResp(interest, 400, "Missing leaf name", nil)
		return
	}
	target := interest.Name()[len(LOCAL_PREFIX)+2:]

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
		Append(target...)

	m.manager.sendStatusDataset(interest, name, enc.Wire{buf})
}
