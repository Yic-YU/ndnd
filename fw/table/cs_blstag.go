package table

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync"

	enc "github.com/named-data/ndnd/std/encoding"
)

// 中文说明：当前阶段我们把“BLSTag”先实现为一个“带密钥的标签”，用于检测 CS 缓存内容是否被静默篡改。
//
// - 目标：当缓存 wire 被修改时，重新计算标签会不一致，从而在挑战阶段被发现并清除。
// - 约束：尽可能最小改动，且不引入复杂/重依赖。
// - 现状：暂用 HMAC-SHA256(key, Name||Wire) 作为 BLSTag 的占位实现；
//   后续若要切换为真正的 BLS 签名/聚合，只需替换 ComputeCsAuditBlstag 的实现即可。
//
// 注意：这里的 key 等价于“本节点私钥”（用户希望用常量定义即可）。

var csAuditBlsKeyOnce sync.Once
var csAuditBlsKey [32]byte

// 默认“私钥”（32 bytes）。
// 中文说明：仅用于实验/调试；生产环境应通过安全方式加载，并避免硬编码。
var csAuditBlsKeyDefault = [32]byte{
	0x3a, 0x1f, 0x8b, 0x23, 0x71, 0x4c, 0x9d, 0x5e,
	0x0f, 0x44, 0x12, 0x9a, 0x6d, 0x2c, 0x80, 0x11,
	0x55, 0x90, 0xe3, 0x7b, 0x6a, 0x0d, 0x2e, 0x4f,
	0x91, 0x0a, 0x7c, 0x3d, 0x18, 0xe6, 0x2b, 0xc0,
}

func cfgCsAuditBlsKey() [32]byte {
	csAuditBlsKeyOnce.Do(func() {
		// 中文说明：允许通过环境变量覆盖（便于实验对比），但默认仍使用常量 key。
		// - NDND_CS_AUDIT_BLS_SK_HEX: 64 hex chars (32 bytes)
		if v := os.Getenv("NDND_CS_AUDIT_BLS_SK_HEX"); v != "" {
			if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
				copy(csAuditBlsKey[:], b)
				return
			}
		}
		csAuditBlsKey = csAuditBlsKeyDefault
	})
	return csAuditBlsKey
}

var csAuditBlsTagDomain = []byte("ndnd-cs-blstag-v1")

// ComputeCsAuditBlstag 计算缓存条目的 BLSTag。
//
// 中文说明：
// - 输入：Name（绑定命名）+ Wire（绑定内容）
// - 输出：32 字节标签（便于和现有 CSNAT/管理接口最小兼容）
func ComputeCsAuditBlstag(name enc.Name, wire []byte) [32]byte {
	key := cfgCsAuditBlsKey()
	mac := hmac.New(sha256.New, key[:])

	// 域分离，避免与其它用途的 HMAC 混用
	mac.Write(csAuditBlsTagDomain)

	// 为避免拼接歧义，加长度前缀
	nameBytes := name.Bytes()
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(nameBytes)))
	mac.Write(u32[:])
	mac.Write(nameBytes)

	binary.BigEndian.PutUint32(u32[:], uint32(len(wire)))
	mac.Write(u32[:])
	mac.Write(wire)

	sum := mac.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}

