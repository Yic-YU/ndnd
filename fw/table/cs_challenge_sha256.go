package table

import (
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/named-data/ndnd/fw/core"
	enc "github.com/named-data/ndnd/std/encoding"
)

// CsSha256Proof 是 CS 对“挑战”给出的证明（当前阶段为 BLSTag，占位实现为 HMAC-SHA256）。
//
// 中文说明：
// - 该证明由 CS 在转发线程内对当前缓存条目重新计算 BLSTag 得到（用于检测缓存静默损坏/篡改）。
// - Auditor 收到后，再与 CSNAT 中记录的“期望标签”对比即可。
type CsSha256Proof struct {
	Name     enc.Name
	Index    uint64
	Computed [32]byte
	Time     time.Time
}

// CsSha256Proofs 是 best-effort 的证明流：CS 把 proof 发给 Auditor（满了就丢弃，不阻塞转发线程）。
var CsSha256Proofs = make(chan CsSha256Proof, 1024)

// csSha256ChallengeReq 用于把“挑战请求”传递给转发线程（由转发线程在 PitCsTree.Update() 中处理）。
var csSha256ChallengeReq = make(chan struct{}, 1)

var csSha256ChallengerOnce sync.Once
var csSha256VerifierOnce sync.Once

// CfgCsSha256ChallengeInterval 通过环境变量配置挑战周期；为空或解析失败则返回 0（不启用）。
//
// 中文说明：
// - 例：NDND_CS_AUDIT_INTERVAL=5s
func CfgCsSha256ChallengeInterval() time.Duration {
	v := os.Getenv("NDND_CS_AUDIT_INTERVAL")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// CfgCsAuditLogEnabled 通过环境变量控制审计流程日志输出（避免默认刷屏）。
//
// 中文说明：
// - 设为 1/true/yes/on 则启用。
// - 例：NDND_CS_AUDIT_LOG=1
func CfgCsAuditLogEnabled() bool {
	switch os.Getenv("NDND_CS_AUDIT_LOG") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

// StartCsSha256Challenger 启动定时挑战器：周期性触发一次“全表重算 BLSTag”的挑战。
//
// 中文说明：
// - 它不会直接访问 CS（避免跨 goroutine 访问 PIT/CS）。
// - 它只发送一个“挑战请求信号”，由转发线程在 Update() 中执行实际的重算并输出 proof。
func StartCsSha256Challenger(interval time.Duration) {
	if interval <= 0 {
		return
	}
	csSha256ChallengerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				if CfgCsAuditLogEnabled() {
					core.Log.Info(nil, "【审计】发起定时挑战", "interval", interval.String())
				}
				select {
				case csSha256ChallengeReq <- struct{}{}:
				default:
				}
			}
		}()
	})
}

// StartCsSha256Verifier 启动证明验证器：从 CsSha256Proofs 接收 proof 并和 CSNAT 的叶子标签对比。
func StartCsSha256Verifier() {
	csSha256VerifierOnce.Do(func() {
		go func() {
			var curTime time.Time
			nOK := 0
			nBad := 0
			nUnknown := 0
			var badSamples []string

			flush := func() {
				if curTime.IsZero() {
					return
				}
				if CfgCsAuditLogEnabled() {
					core.Log.Info(nil, "【审计】验证结果汇总",
						"time", curTime.Format(time.RFC3339Nano),
						"ok", nOK,
						"bad", nBad,
						"unknown", nUnknown,
						"badSamples", badSamples,
					)
				}
				curTime = time.Time{}
				nOK, nBad, nUnknown = 0, 0, 0
				badSamples = nil
			}

			for proof := range CsSha256Proofs {
				if curTime.IsZero() {
					curTime = proof.Time
				} else if !proof.Time.Equal(curTime) {
					flush()
					curTime = proof.Time
				}

				expected, ok := GetCsNatSha256Leaf(proof.Name)
				if !ok {
					nUnknown++
					continue
				}
				if expected != proof.Computed {
					nBad++
					if len(badSamples) < 5 {
						badSamples = append(badSamples,
							proof.Name.String()+": exp="+hex.EncodeToString(expected[:8])+" got="+hex.EncodeToString(proof.Computed[:8]))
					}
					core.Log.Warn(nil, "【审计】校验失败（BLSTag 不一致）", "name", proof.Name)
				} else if CfgCsAuditLogEnabled() {
					nOK++
				}
			}
		}()
	})
}

func publishCsSha256Proof(p CsSha256Proof) {
	select {
	case CsSha256Proofs <- p:
	default:
	}
}

func popCsSha256ChallengeReq() bool {
	select {
	case <-csSha256ChallengeReq:
		return true
	default:
		return false
	}
}
