package table

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"sync"
	"time"

	enc "github.com/named-data/ndnd/std/encoding"
)

// CsNatSha256Tree 是一个“内容存储命名审计树”（CSNAT）的最小实现（SHA-256 版本）。
//
// 中文说明：
//   - 结构：按 NDN Name 组件分层的多叉树/Trie（每一层一个 component）。
//   - 叶子：每个被缓存的 Data name（精确名）对应一个叶子条目（节点可同时有 children）。
//   - 标签：这里用 sha256(DataWire) 作为临时审计标签（可视为 BLSTag 的占位实现）。
//   - 聚合：SHA-256 不具备同态加法，因此用“Merkle 化”的聚合：父节点的 Agg 由自己的叶子标签 + 所有子节点的 Agg 计算得到，
//     支持按 Prefix 查询子树的聚合值。
type CsNatSha256Tree struct {
	mu   sync.RWMutex
	root *csNatSha256Node

	// 统计信息（用于日志/调试）
	nodeCount       uint64 // 树节点数量（含 root）
	activeLeafCount uint64 // leafCount>0 的节点数量
}

type csNatSha256Node struct {
	parent *csNatSha256Node

	// component 的 TLV 编码（type+len+value），用于：
	// 1) 唯一标识 child（作为 map key 的来源）
	// 2) 参与父节点聚合哈希，绑定树结构
	compWire []byte

	children map[string]*csNatSha256Node // key: string(component TLV bytes)

	leafCount uint32
	leafTag   [32]byte
	staleTime time.Time

	agg [32]byte
}

var csNatSha256Domain = []byte("ndnd-csnat-sha256-v1")

func newCsNatSha256Tree() *CsNatSha256Tree {
	t := &CsNatSha256Tree{
		root: &csNatSha256Node{
			children: make(map[string]*csNatSha256Node),
		},
	}
	t.nodeCount = 1
	// 空树的根节点也有确定的聚合值
	t.root.recomputeAgg()
	return t
}

func (n *csNatSha256Node) childKeysSorted() []string {
	if len(n.children) == 0 {
		return nil
	}
	keys := make([]string, 0, len(n.children))
	for k := range n.children {
		keys = append(keys, k)
	}
	// key 是 component 的 TLV bytes 转成的 string；排序等价于按 bytes 字典序排序，稳定且可复现。
	sort.Strings(keys)
	return keys
}

func (n *csNatSha256Node) recomputeAgg() {
	h := sha256.New()
	h.Write(csNatSha256Domain)

	// leafCount 用于表达“该 Name 在多少个 CS 实例中存在”（多线程时可能>1）。
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], n.leafCount)
	h.Write(u32[:])
	if n.leafCount > 0 {
		h.Write(n.leafTag[:])
	}

	// 为避免拼接歧义，加入 child 数量与每个 component 的长度前缀
	binary.BigEndian.PutUint32(u32[:], uint32(len(n.children)))
	h.Write(u32[:])

	for _, k := range n.childKeysSorted() {
		ch := n.children[k]

		binary.BigEndian.PutUint32(u32[:], uint32(len(ch.compWire)))
		h.Write(u32[:])
		h.Write(ch.compWire)
		h.Write(ch.agg[:])
	}

	sum := h.Sum(nil)
	copy(n.agg[:], sum)
}

func (t *CsNatSha256Tree) findOrCreateNodeLocked(name enc.Name) *csNatSha256Node {
	cur := t.root
	for _, comp := range name {
		compBytes := comp.Bytes()
		key := string(compBytes)

		child, ok := cur.children[key]
		if !ok {
			compWire := make([]byte, len(compBytes))
			copy(compWire, compBytes)

			child = &csNatSha256Node{
				parent:   cur,
				compWire: compWire,
				children: make(map[string]*csNatSha256Node),
			}
			cur.children[key] = child
			t.nodeCount++
		}
		cur = child
	}
	return cur
}

func (t *CsNatSha256Tree) findNodeLocked(prefix enc.Name) *csNatSha256Node {
	cur := t.root
	for _, comp := range prefix {
		key := string(comp.Bytes())
		child, ok := cur.children[key]
		if !ok {
			return nil
		}
		cur = child
	}
	return cur
}

// OnInsert 用于处理“首次入缓存”事件：增加 leafCount，并更新叶子标签，然后自底向上重算聚合值。
func (t *CsNatSha256Tree) OnInsert(name enc.Name, tag [32]byte, staleTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	leaf := t.findOrCreateNodeLocked(name)
	if leaf.leafCount == 0 {
		t.activeLeafCount++
	}
	leaf.leafCount++
	leaf.leafTag = tag
	leaf.staleTime = staleTime

	// 自底向上更新聚合值
	for n := leaf; n != nil; n = n.parent {
		n.recomputeAgg()
	}
}

// OnRefresh 用于处理“同名覆盖/刷新缓存”事件：不改变 leafCount，只更新标签并重算。
func (t *CsNatSha256Tree) OnRefresh(name enc.Name, tag [32]byte, staleTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	leaf := t.findOrCreateNodeLocked(name)
	if leaf.leafCount == 0 {
		// 容错：若审计端漏了 Insert 事件，把 Refresh 当作 Insert。
		leaf.leafCount = 1
		t.activeLeafCount++
	}
	leaf.leafTag = tag
	leaf.staleTime = staleTime

	for n := leaf; n != nil; n = n.parent {
		n.recomputeAgg()
	}
}

// OnErase 用于处理“淘汰/删除”事件：减少 leafCount；当 leafCount 归零时清空并剪枝，然后自底向上重算聚合值。
//
// 中文说明：
// - 该操作用于处理 CS 的淘汰/删除事件（例如 LRU Evict）。
// - 若该 Name 不存在或 leafCount 已为 0，则返回 false。
func (t *CsNatSha256Tree) OnErase(name enc.Name) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	leaf := t.findNodeLocked(name)
	if leaf == nil || leaf.leafCount == 0 {
		return false
	}

	leaf.leafCount--
	if leaf.leafCount == 0 {
		leaf.staleTime = time.Time{}
		if t.activeLeafCount > 0 {
			t.activeLeafCount--
		}
	}

	// 从叶子开始向上剪枝：只要节点既不是叶子也没有子节点，就可以从父节点 children 中移除。
	// 注意：这里不需要回收内存，Go GC 会处理。
	cur := leaf
	for cur.parent != nil && cur.leafCount == 0 && len(cur.children) == 0 {
		parent := cur.parent
		delete(parent.children, string(cur.compWire))
		if t.nodeCount > 0 {
			t.nodeCount--
		}
		cur = parent
	}

	// 从“仍然存在的最低层节点”开始，向上重算聚合值。
	for n := cur; n != nil; n = n.parent {
		n.recomputeAgg()
	}
	return true
}

// GetAggregatedTagByPrefix 查询某个前缀子树的聚合标签（SHA-256）。
func (t *CsNatSha256Tree) GetAggregatedTagByPrefix(prefix enc.Name) ([32]byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.findNodeLocked(prefix)
	if node == nil {
		return [32]byte{}, false
	}
	return node.agg, true
}

// GetLeafTag 查询某个精确 Name 对应的叶子标签（如果该 Name 处有缓存条目）。
func (t *CsNatSha256Tree) GetLeafTag(name enc.Name) ([32]byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.findNodeLocked(name)
	if node == nil || node.leafCount == 0 {
		return [32]byte{}, false
	}
	return node.leafTag, true
}

// Stats 返回 CSNAT 的调试统计信息（节点数、有效叶子数、根聚合值）。
func (t *CsNatSha256Tree) Stats() (nodeCount uint64, activeLeafCount uint64, rootAgg [32]byte) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodeCount, t.activeLeafCount, t.root.agg
}
