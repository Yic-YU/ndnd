package table

import (
	"bytes"
	"testing"
	"time"

	"github.com/named-data/ndnd/fw/defn"
	enc "github.com/named-data/ndnd/std/encoding"
	"github.com/stretchr/testify/assert"
)

func drainCsAuditEvents() {
	for {
		select {
		case <-CsAuditEvents:
		default:
			return
		}
	}
}

func TestCsAuditEventsOnInsertAndRefresh(t *testing.T) {
	drainCsAuditEvents()

	setReplacementPolicy("lru")
	CfgSetCsCapacity(1024)

	pitCS := NewPitCS(func(PitEntry) {})

	pkt, _ := defn.ParseFwPacket(enc.NewBufferView(VALID_DATA_1), false)
	data1 := pkt.Data

	pitCS.InsertData(data1, VALID_DATA_1)
	var ev1 CsAuditEvent
	select {
	case ev1 = <-CsAuditEvents:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for CsAuditEventInsert")
	}
	assert.Equal(t, CsAuditEventInsert, ev1.Type)
	assert.True(t, bytes.Equal(VALID_DATA_1, ev1.Wire))
	assert.Equal(t, data1.NameV.Hash(), ev1.Index)
	assert.Equal(t, data1.NameV.String(), ev1.Name.String())

	pitCS.InsertData(data1, VALID_DATA_1)
	var ev2 CsAuditEvent
	select {
	case ev2 = <-CsAuditEvents:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for CsAuditEventRefresh")
	}
	assert.Equal(t, CsAuditEventRefresh, ev2.Type)
	assert.True(t, bytes.Equal(VALID_DATA_1, ev2.Wire))
	assert.Equal(t, data1.NameV.Hash(), ev2.Index)
	assert.Equal(t, data1.NameV.String(), ev2.Name.String())
}
