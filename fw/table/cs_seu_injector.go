package table

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/named-data/ndnd/fw/core"
	enc "github.com/named-data/ndnd/std/encoding"
)

// 中文说明：SEU（Single Event Upset）比特翻转注入器（泊松过程）。
//
// 目标：用给定的 SEU 率 r（bit^-1·day^-1）模拟缓存内容的静默比特翻转，
//      以验证审计机制（BLSTag 重算对比）能够检测并清除损坏条目。
//
// 模型：
// - 每个 bit 的翻转是独立泊松过程，率为 r（按天计）。
// - 若当前 CS 中总比特数为 B，则“系统发生一次翻转事件”的到达率为 λ = r * B（按天计）。
// - 等价地，事件间隔服从指数分布：Δt ~ Exp(λ_sec)，其中 λ_sec = λ / 86400。
//
// 注意：
// - 注入翻转不会发布 CsAuditEvent（因为它模拟“静默损坏”），因此 CSNAT 中仍保存旧标签；
//   下一次挑战会重算标签并触发不一致，从而删除条目。

const csSeuDefaultRatePerBitPerDay = 1.51e-7

func cfgCsSeuEnabled() bool {
	switch os.Getenv("NDND_CS_SEU_ENABLE") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

func cfgCsSeuLogEnabled() bool {
	switch os.Getenv("NDND_CS_SEU_LOG") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		// 中文说明：若未单独开启 SEU 日志，则跟随审计日志开关，方便调试。
		return CfgCsAuditLogEnabled()
	}
}

func cfgCsSeuRatePerBitPerDay() float64 {
	v := os.Getenv("NDND_CS_SEU_RATE_PER_BIT_PER_DAY")
	if v == "" {
		return csSeuDefaultRatePerBitPerDay
	}
	r, err := strconv.ParseFloat(v, 64)
	if err != nil || r <= 0 || math.IsNaN(r) || math.IsInf(r, 0) {
		return csSeuDefaultRatePerBitPerDay
	}
	return r
}

func cfgCsSeuPrefix() enc.Name {
	// 默认只对业务前缀注入，避免影响 /localhost 管理面与路由控制面。
	v := os.Getenv("NDND_CS_SEU_PREFIX")
	if v == "" {
		v = "/minindn"
	}
	p, err := enc.NameFromStr(v)
	if err != nil {
		return enc.Name{}
	}
	return p
}

// randUint64 returns a random uint64 (best-effort; on failure returns 0).
func randUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

// randFloat64Open01 returns a float64 in (0,1), best-effort.
func randFloat64Open01() float64 {
	u := randUint64()
	// 将 [0,2^64-1] 映射到 (0,1)；避免 0 导致 ln(0)。
	if u == 0 {
		u = 1
	}
	return float64(u) / (float64(^uint64(0)) + 1.0)
}

func sampleExpDuration(lambdaPerSec float64) time.Duration {
	if lambdaPerSec <= 0 || math.IsNaN(lambdaPerSec) || math.IsInf(lambdaPerSec, 0) {
		return 0
	}
	u := randFloat64Open01()
	sec := -math.Log(u) / lambdaPerSec
	if sec < 0 {
		sec = 0
	}
	// 防止极小 λ 导致 Duration 溢出（Duration 最大约 290 年）
	const max = 200 * 365 * 24 * time.Hour
	d := time.Duration(sec * float64(time.Second))
	if d > max {
		return max
	}
	return d
}

// seuMaybeInject flips one random bit in CS according to SEU Poisson process.
// It must be called from the forwarding thread (PitCsTree.Update()).
func (p *PitCsTree) seuMaybeInject(now time.Time) {
	if !cfgCsSeuEnabled() {
		return
	}
	if !p.csSeuNext.IsZero() && now.Before(p.csSeuNext) {
		return
	}

	prefix := cfgCsSeuPrefix()

	// 计算当前可注入集合的总 bit 数 B
	var totalBits uint64 = 0
	for _, entry := range p.csMap {
		if entry == nil || len(entry.wire) == 0 {
			continue
		}
		if len(prefix) > 0 && !prefix.IsPrefix(entry.node.name) {
			continue
		}
		totalBits += uint64(len(entry.wire)) * 8
	}

	// 若当前没有可注入条目，则稍后再检查（避免空转）
	if totalBits == 0 {
		p.csSeuNext = now.Add(30 * time.Second)
		return
	}

	// 发生一次翻转事件：按 bit 均匀选择目标
	targetBit := randUint64() % totalBits
	var curBits uint64 = 0
	for _, entry := range p.csMap {
		if entry == nil || len(entry.wire) == 0 {
			continue
		}
		if len(prefix) > 0 && !prefix.IsPrefix(entry.node.name) {
			continue
		}

		entryBits := uint64(len(entry.wire)) * 8
		if targetBit >= curBits+entryBits {
			curBits += entryBits
			continue
		}

		off := targetBit - curBits
		byteIndex := int(off / 8)
		bitIndex := int(off % 8)
		mask := byte(1 << uint(bitIndex))

		oldB := entry.wire[byteIndex]
		newB := oldB ^ mask
		entry.wire[byteIndex] = newB

		if cfgCsSeuLogEnabled() {
			core.Log.Info(nil, "【审计】SEU 注入：随机比特翻转（泊松过程）",
				"name", entry.node.name,
				"byteIndex", byteIndex,
				"bitIndex", bitIndex,
				"old", oldB,
				"new", newB,
				"totalBits", totalBits,
				"prefix", prefix.String(),
			)
		}
		break
	}

	// 采样下一次事件时间：λ_sec = (r/86400)*B
	rPerDay := cfgCsSeuRatePerBitPerDay()
	lambdaPerSec := (rPerDay / 86400.0) * float64(totalBits)
	delta := sampleExpDuration(lambdaPerSec)
	if delta <= 0 {
		delta = 30 * time.Second
	}
	p.csSeuNext = now.Add(delta)
}
