package table

import (
	"time"

	enc "github.com/named-data/ndnd/std/encoding"
)

// CsAuditEventType indicates what kind of CS mutation occurred.
type CsAuditEventType uint8

const (
	CsAuditEventInsert CsAuditEventType = iota + 1
	CsAuditEventRefresh
	CsAuditEventErase
)

// CsAuditEvent is published when the CS stores or refreshes a Data packet.
// Wire is a private copy of the cached packet's raw wire encoding (may be nil for Erase).
type CsAuditEvent struct {
	Type      CsAuditEventType
	Name      enc.Name
	Index     uint64
	Wire      []byte
	StaleTime time.Time
}

// CsAuditEvents is a best-effort event stream for CS audit consumers.
// Producers never block: when the channel is full, events are dropped.
//
// 中文说明：
// - 该通道用于在“数据进入 CS/刷新 CS”时把原始 Data 的 wire（TLV 编码）发送给审计模块。
// - 事件发送方（转发线程）绝不阻塞：如果通道满了就直接丢弃事件，以免影响转发性能。
var CsAuditEvents = make(chan CsAuditEvent, 1024)

func publishCsAuditEvent(ev CsAuditEvent) {
	// 中文说明：非阻塞发送；通道满时丢弃该事件。
	select {
	case CsAuditEvents <- ev:
	default:
	}
}
