# **面向深空 NDN 网络的缓存数据完整性验证实施方案**

## **1\. 概述**

本方案旨在现有的 Mininet \+ NDN 仿真环境中，集成论文提出的 **双签名审计机制** 与 **CSNAT (基于内容存储命名的审计树)**。

**核心目标：**

1. **生产者 (Producer)**：在数据包中嵌入 BLS 审计标签 (![][image1])。  
2. **缓存节点 (Content Store Agent)**：利用 Goroutine 监听缓存数据，构建并维护 CSNAT。  
3. **审计者 (Auditor)**：定期发起挑战，验证聚合标签的正确性，模拟检测宇宙射线导致的数据损坏。

## **2\. 核心数据结构 (Go Language Definition)**

在 Go 程序中，我们需要引入 BLS 密码学库（推荐 github.com/kilic/bls12-381 或类似库）来实现标签的聚合运算。

### **2.1. 审计标签与数据包扩展**

NDN 标准数据包需要携带额外的审计标签。在应用层模拟时，可定义如下结构：

import (  
    "sync"  
    "\[github.com/kilic/bls12-381\](https://github.com/kilic/bls12-381)" // 假设使用此库进行BLS运算  
)

// DeepSpaceDataPacket 模拟封装后的 NDN 数据包  
type DeepSpaceDataPacket struct {  
    Name      string // NDN 名字，如 "/com/google/map/v1"  
    Content   \[\]byte // 实际数据内容  
    Timestamp int64  // 时间戳  
      
    // \--- 论文核心新增字段 \---  
    ECDSASig  \[\]byte // 标准源认证签名 (用于消费者验证)  
    BLSTag    \[\]byte // 审计标签 σ (G1 Point, 用于 CSNAT 聚合)  
}

### **2.2. CSNAT (内容存储命名审计树) 节点定义**

这是论文图 3 和图 6 的代码实现。

// CSNATNode 代表审计树的一个节点  
type CSNATNode struct {  
    // 基础属性  
    Component string               // 路径组件，例如 "Tele" 或 "Temp"  
    FullPrefix string              // 当前节点的完整前缀，如 "/Root/Tele"  
      
    // 聚合值 (核心逻辑)  
    // 对应论文公式：Parent.Value ← Parent.Value \+ σi  
    AggregatedValue \*bls12381.PointG1   
      
    // 树结构指针  
    Children map\[string\]\*CSNATNode // 子节点映射  
    Parent   \*CSNATNode            // 父节点指针 (用于向上递归更新)  
      
    // 状态标记  
    IsLeaf bool // 是否为叶子节点（具体的数据包）  
}

// CSNATTree 管理整棵树  
type CSNATTree struct {  
    Root \*CSNATNode  
    Lock sync.RWMutex // 读写锁，保证 Goroutine 并发安全  
}

## **3\. 系统流程与逻辑实现**

### **3.1. 阶段一：生产者逻辑 (Producer)**

**位置**：Mininet 中的 Producer Host (如 h1)。

**动作**：在发送数据前，计算 BLS 标签。

1. **准备数据**：切分文件/数据为 ![][image2]。  
2. **计算 Hash**：![][image3]。  
3. **计算标签**：![][image4] (其中 ![][image5] 为审计私钥)。  
4. **封装发送**：将 ![][image1] 序列化后放入 DeepSpaceDataPacket 的 BLSTag 字段，通过 NDN put 发送。

### **3.2. 阶段二：缓存节点逻辑 (CS Agent) \- 核心 Goroutine**

**位置**：Mininet 中的深空节点/卫星 (如 s1 或 h2 acting as router)。

**机制**：这是一个常驻进程，包含两个主要 Goroutine。

#### **Goroutine A: 监听与构建 (Build Tree)**

当节点接收/缓存数据时触发（模拟拦截）。

// 伪代码流程  
func (agent \*CSAgent) OnDataCached(packet DeepSpaceDataPacket) {  
    // 1\. 解析数据  
    nameComponents := SplitName(packet.Name) // e.g., \["Root", "Tele", "Temp"\]  
    tag := DeserializeBLS(packet.BLSTag)     // 反序列化标签 σ  
      
    agent.Tree.Lock.Lock()  
    defer agent.Tree.Lock.Unlock()  
      
    // 2\. 插入或更新 CSNAT  
    currentNode := agent.Tree.Root  
      
    // 3\. 自底向上更新聚合值 (论文核心)  
    // 这里采用先递归找到位置，再回溯更新的方式  
    agent.RecursiveInsertAndUpdate(currentNode, nameComponents, tag)  
}

func (agent \*CSAgent) RecursiveInsertAndUpdate(node \*CSNATNode, components \[\]string, newTag \*PointG1) {  
    // 累加当前节点的聚合值： Node.Val \= Node.Val \+ newTag (同态加密特性)  
    bls.Add(node.AggregatedValue, node.AggregatedValue, newTag)  
      
    if len(components) \== 0 {  
        node.IsLeaf \= true  
        return  
    }  
      
    // 寻找下一跳，若不存在则创建  
    nextComp := components\[0\]  
    if child, exists := node.Children\[nextComp\]; exists {  
        RecursiveInsertAndUpdate(child, components\[1:\], newTag)  
    } else {  
        newChild := NewNode(nextComp)  
        node.Children\[nextComp\] \= newChild  
        RecursiveInsertAndUpdate(newChild, components\[1:\], newTag)  
    }  
}

#### **Goroutine B: 审计响应 (Audit Responder)**

监听来自 Auditor 的挑战请求。

1. **接收挑战**：收到包含 Prefix (如 /Root/Tele) 和随机数 ChallengeNum 的请求。  
2. **查询树**：在 CSNAT 中找到对应 Prefix 的节点。  
3. **生成证明**：  
   * 获取该节点的 AggregatedValue。  
   * (可选) 根据论文，可能需要提供辅助路径证明。  
4. **返回证明**：将证明发回 Auditor。

### **3.3. 阶段三：审计者逻辑 (Auditor)**

**位置**：Mininet 中的地面站或第三方审计节点。

**机制**：定时任务。

1. **发起挑战 (Challenge)**：  
   * 随机选择一个命名前缀 ![][image6]。  
   * 发送 ![][image7] 给缓存节点。  
2. **验证证明 (Verify)**：  
   * 接收 CS 返回的聚合标签 ![][image8]。  
   * **验证公式**：利用双线性对 (Bilinear Pairing) 验证 ![][image9]。  
   * *注意：* 在代码实现中，验证者需要知道该前缀下包含哪些文件，或者由 CS 提供这些元信息。

## **4\. 模拟实验设计 (Simulation)**

为了验证论文效果，你需要在代码中增加“故障注入”功能。

### **4.1. 故障模拟 (Bit Flip)**

在缓存节点的 Go 程序中增加一个“宇宙射线”函数：

func (agent \*CSAgent) SimulateCosmicRay() {  
    // 随机选择树中的一个叶子节点  
    targetNode := agent.Tree.GetRandomLeaf()  
      
    // 模拟数据损坏：修改其 Tag 或 Content  
    // 注意：只修改数据，但不通过正常流程更新树的聚合值  
    // 这样会导致 Tree.Root 的聚合值与实际数据的 Hash 不匹配  
    targetNode.CorruptData()   
      
    fmt.Println("Simulated radiation damage on:", targetNode.FullPrefix)  
}

### **4.2. 实验步骤**

1. **Setup**: 启动 Mininet，运行 Go 程序构建 Producer, CS, Auditor。  
2. **Put**: Producer 发送 100 个数据包。  
3. **Observe**: 观察 CS Agent 的日志，确认 CSNAT 树已建立，根节点有聚合值。  
4. **Audit (Success)**: Auditor 发起挑战，验证通过。  
5. **Inject Fault**: 调用 SimulateCosmicRay() 损坏某个数据。  
6. **Audit (Fail)**: Auditor 再次发起挑战，由于数据被改但聚合值未合法更新（或聚合值被改但数据未改），双线性对验证将失败。  
7. **Recovery**: 触发重传逻辑（从 Producer 重新获取数据并更新树）。

## **5\. 总结：Go 代码工程目录建议**

/ndn-deepspace-audit  
├── main.go            \# 程序入口，根据参数启动 Producer/CS/Auditor 模式  
├── core/  
│   ├── types.go       \# 定义 DeepSpaceDataPacket, Keys  
│   └── crypto.go      \# 封装 BLS 签名、聚合、验证函数  
├── csnat/  
│   ├── tree.go        \# CSNAT 树结构定义  
│   └── operations.go  \# Insert, Update, GetProof 方法  
└── roles/  
    ├── producer.go    \# 负责生成带 Tag 的数据包  
    ├── cs\_agent.go    \# 负责缓存监听、建树、故障模拟 (Goroutine 所在地)  
    └── auditor.go     \# 负责定时挑战、验证逻辑  


[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAAYCAYAAADOMhxqAAAA4UlEQVR4XmNgGAUjFBgbG7MqKiqKy8vLSyJjBQUFARSF4uLi3HJychOAkn+B+D8WvAekBqxYSUmJHyQAxOdlZWVtgTYAmfJLgfgtEFsCsaSoqCgPzHBGqMlXlJWVxWCCQDFjoNgnIPaEiYEBUEATalIOsjhQgw1Q7DeQ9kUWB0n4AiW+AZ1iiiwO9GQ61CBNZHGYhocgd8LEZGRkOIHiO4Bic4BcFiTlDAxAk3WAEjdANFQI5KcyoNhFkOdRFEMBI1AyC4jPAPEsID4AdM5MoC1C6ApRAFARB8hZIBpdbrgBAASONTLwDBdkAAAAAElFTkSuQmCC>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABUAAAAYCAYAAAAVibZIAAABfklEQVR4Xu2TP0sDQRDFE0ihoI0ahNwle7E5LEOw8Q9YaBlL/QA21jbRzsZCEMHaUiRNSgURC7+EpWARsQqCqIWi8fcue3JZES6lcA8euzfzZnb2bZLLZcgQwRgzDx9hz/K2VCpNuboYlUplFc2n1Wq99jxv0tVFIHkIH2AHke/mBRWTb8Fn2CZUcDU/KBaLY4hOgyA4Zn1lmrqrAXnyW3AXzRfcdgUDoMkMohPWdV2LteFqiNds0x32H2gWXc0AyuXymiZgnaPgzZ3C9/1R8nusHrkLeFetVqeTml9QASevIJ6FXbjv5DeUt03vTVo/9Tg6XVPAM1J55WkY0LDJtmAPTu8ntSP2gBtR+1y/UVONrbZphvHTfuY1ZeyZJtPVldChZlg/42/5CbvEl+GBHklx2UO8Y1L62ZIFcUx+mf4voA1rcXwYPxcQnYdhOJ6INYj1rCXRYwn2Bn/7iY9LCJ5UbPkON5WjqE7Dq/j/TPwIviS1aC6xZWKwa4Z/gW9DvnfkrXSMRgAAAABJRU5ErkJggg==>

[image3]: <data:image/png;base64,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>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAEsAAAAYCAYAAACyVACzAAAC6UlEQVR4Xu2XP2hTURTGX2jF+ge1agxN2twkzaK4RQWhugmKVIoIKu4KRRxasKuLm4PaQZSAk1uxg4tghw7iYkEERQcFFRFEuggKokV/X9994eY2CZFoing/+Hh555x73nnnnnPuSxQFBAQEBAT8B0jlcrlthUJhi68IcGCMuQJ/Wk74+gAPJOk4/J7P50d8XYAHEnUdvhkcHMz5ugAH6XR6I4mahzPc9vr6hqhUKmuKxWKGRQMuV3PoKaahoaE9xDAGC4hSvk2n4B13wkVacErvz3WU60E927eNMpnMBgyusmDJDjmfc7Lx1/1lpMjNSZ79Ck7w+xTXh8R5w75Er5JYLpfX+gt/F/g5Zt99Tv7tsx7Be7TlupphqVTaLCP4hEUHyCg/zR24CPfDAZWp47sOOCtjs8BD3rVLgjnt+/GQwuck/EQ8+xKhjecz6w/jp8Tv6ajdtmkBE8+rJW1OZCsX/1PI3ur9E7uUrahnw8PDOxIhsoqCgkcSWTdhn/+F4C+5cgVuX6AKx1UBrt6Fqg6b1/i64OtcOPNq1m077i9rvdoyESz3KjxfWx0tBzti4mN01JV3Cybe6RXHOPf9yB+buBOq6gpX7wL9hIlHyCxJ7fP1CZIcqJISWcOBr2Qg+Mou7E0MBZyfkwM5cuVN0KOqNN6h0Iqt2toJ9Hk2m93eRLdE7IdcnQ/NGg1pZtomX+fCzqtv7sYklQ3P1Axtsur6Ug9Bfh9ZNWpjHmjwY3uUNSfapWmxCdafKmfeT2qzlukEJm63uu8rtT+y97CoTYFnldXdCF7qau00wy4ie6pBnyzuNvLxcH1RmxdAFYJsGv5QwvQ/DrtdUQefEo3azZVR2etJ3DUdYtLp1BmHC/CWjFDeRLm1zmuXYT9lbmvTbFx34QMlJx93w0fdw8mog2TZk/+DqZ/ZScHok2WGfIw5uuWy6zPxx2fTQbgasLu8Ii7JC3/mQ1mJ6efa4yvk3x8DAQEBAf8KfgHOvNxl4v3gQwAAAABJRU5ErkJggg==>

[image5]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAAZCAYAAAAFbs/PAAABBUlEQVR4XmNgGAUjFCgqKorLycn5KigoOAAxB7o8HAAlJeTl5dcA8XogOwJIVwPxE1lZWROQvIyMjDQQq5KnAegMoJz8FSCeZWxszAo1gwXIXw503g6gQk4guwbItoFJzAGZBsSKUMVgAFRQDhT7BLTRA8ier6SkxM8AFNAE4rcg54A0I2sAikUD8VeQYqCmcJgpvkDB/0CBdGTFyHJAvBnkLLAgkOMJEgRJoqmHafgDNMweLiglJSULFLwNxDlIahmBfCcgvgEzTFpaWgaoVgQsC5W8D8SrgXguEJ8A4mIVFRVRIL0NaMMpUGiBQhPJUAZmZWVlMaBJwiAbkMVBJiMF95AHAELKQyZBhU/lAAAAAElFTkSuQmCC>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA8AAAAYCAYAAAAlBadpAAABDElEQVR4XmNgGAUDBOTl5R2B+DkQ/0fC74D4FZT9RU5OboKKigoful44ACqaA8S/gQpt0MSNQAYpKCjsEhUV5UGWAwN1dXVeoILDQHxXUVFRHFkOpAEofgCI/wENdkGWAwOghCYQvwXiNUAuC7IcUIMgUPw0NleBgaysrB/Uf0XockAxSyD+CcQnlJSU+NHlQQomYTMZpBgovgeIXwMtMEGWAwMkP4FMXwrEs0AYGEALgfQzoIHzpaWlZdD1gYE8wr+7gYEFpOQlYRhoAAe6ehSA5N8qdDmCQB7iX+zRgA8gxe8DGRkZaXR5vADoZB2gxvdAvJWg/2AAqMkWqOEh1K8wDErPyehqRwGFAACUK085U395ygAAAABJRU5ErkJggg==>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAF8AAAAYCAYAAACcESEhAAAEwklEQVR4Xu2ZW2hcVRSGZ2gU7+Ilhtxm5aahUdESFRQVFJEGrQ+1FaX64oNVFBSLihahUgr6oEgpRIuCCtqXiBTx8iCi9KWooIL1QSteKAYqNigqVDH6/Tn7tHtWz5w5k85kWpgffiZn//uyztp7r732SanUQQcddNBBB8cBhoaGTjKz3u7u7tO81kaU+/v7zxkeHu6ZnJw8wYuFoU4GBwdvrFQqa3jJ5RQt83XagZGRkTOx53X4O7a9wCRc6uu0EV3YsxLbXpN9cGMjk1Bm1q6lg09o+B5cF/gh3MtkXO4bLDWwbRO27B8YGDjfa0JfX98pYXL+hv8F/gv3RWWfw+upXvbtmwV8dQ1j/IW9t3vtCGiGqPgMDb73TpZG+XY4B1fE2lIDG1/Fho/qhRsW0SWWrL4ZHrvScibtZHbM85oQ3vOWqElTQf+98EfGesxrVZBzqTRN5QMYfYXXBQy9KDh/W6mFK6YeijqfOqstWeUPe413XRW0d3R2eL0ZKOx8DLiXivP69VqKtDO4h619rteXCg04fyv8h5e/OkNTKJXzq3ZFM1HI+WzDMSr9DL8aHR09z+spIueLvV5fKhRxvjTVgd8p83ByF+U75PxC8ThgbGzsDNpMsfAG9axoQTS4TLsoY4xizh9KDjAZsslrMch+Biw5tI5556Mvh79axsqm7FZLdsR00UxEYzHui/Ap+YC2d/H7Nr/38LsR/sbfN8Rt6jo/WiGZ2zOGdNWDu8bHx0/3egr0J6n7UwN8n913tu8nCyHN3A3fyovVOkgtCSufWZIopFzYDYx5W6mB1JmxVsL10SH+gWyRZjWcHNm6o5QV2tKGgbmrWZ1bgR3SCsjRSi0Z/w34tSX3jpqwJN7PY/Pa8I4L1N2ltIhkgfHvC47XIa4FeF2qyRZLdtkRB7vCEuWz2PG0IkcpnvBglByfu5qjWfyFji70equhuwfj7oTfYMNNpRwHRrt5X3jhpsGSSa06R8IuO1jJiBxKa5m4h9D3wxn8eMEhUVkLhXt8hx50fCd15uEGr3mElz+02uoxHPKFQgD1r4IH8uywwyuxqWmkFid97jJ3jljGhAR04bdpLRglNU5bQHryzymPD5epzXAvfFbPyvulw+1FDigGupgB1xQl/U5phfh+akD2zljOgWshv6/UOuhqQP3lhSXLCC/RLss62NOo8kRcXgXEFalzWSl3YPRalVeSE/1++AN8rgEHtRR1sp0y2jZL4n1V9pEHOZ02X8KD8EqvCxbifSUKL5YxIZG24Py6i0CfE6j4rTqHW+A6Gu20ZAek30CWFVn5rUaW8xVesPcVyv+w6HsOZR8XuRCGFfyuJaH1Ea8LSjTMXTDz4r0Vdb4QXRoUDlZVkhx2S6rzfLP0uE07kOX8ZkG7hb4f8OWCJtiPKf9YdrxvzPkeNLwb/gk3WHKovHkshJ4WO/9RqxF2PPLivXC0zp+yw1t4Tgevr9MOBOd/ykud5bWjQcjjXyq6wBh/hPqz2LPea0L6RWBRzu/p6TmVjh+ng610MOH1diHEWZ1Nm2Wj1xcLhdpG7gVDyT9N9Flh0mvhANf/FGatzoXwuEP4GKh4+4Xl5PwtRnliYuLEuEDP2PMy3M2kPBhS10z8D38CmXK98XJqAAAAAElFTkSuQmCC>

[image8]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACEAAAAYCAYAAAB0kZQKAAAB6klEQVR4Xu2VO0tDQRCFE1CJKChqDHnePKoEu2AhRAlYqIUWsRH8AYJYWFlYCiJYiZUGQSTYiJ0aLSwEWzEKKWz9ATYWNiL6TbJXlzVXVJLuHjjs7NnZ2cnO7I3H48KFCxcu/olsNtueSCQClmUFdcbj8V7Tt+kIBAJdsVhsiwPf4HsDXoqPua9pSCaTPXIIrESj0VFuAtM6hE9wBAb9fn+3ua+Z8KobqKZSqUFbRMuiPcMp3bkl4JC0+sVLuk4SObRXxmldbwnkEA57oQzDuk4jLqjk0rreEqgkHqXuthaJRDrRL9D2mLZp7rU1SRAesV6QsmnLXvppDL0E1/DJi/8Peh3cwBALDzLagQi8gnYvDfrpCKQ50U8JMil+Vr151+117Fn27sgzFx/mlVAoNOCka6FrwRbhDSzCKxx3ybRPdxKgz7FeVrfhwz6xVOMSNIp9J70kc+x5eMyzlqf2TfcYN1yDCiofJZ+5ZoO1A3xWxSaRMPY1wZMyl2Rg1f6F2Nvxel811L+i/hGShP1a1LWeMx/HzqvXVJaShcPhfpVgzkk3Y/8aHDZBkBLjMmNRmhduyPdF6o22yXzfqpepVncn3Yz9J0hQ+/OdyWQ6ZG76cFDBalB3J70pUNd8KyVSfwFnvLYZJ93c/wGXn490huoPHAAAAABJRU5ErkJggg==>

[image9]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAATQAAAAYCAYAAAB0mAFmAAANyklEQVR4Xu1cDaxcRRW+L0Xjv4DW2r97dttq06IRUkURbaABAiGKaUXrX2JCVNQKkQqG4k+raYwFWqQVKJaUamgVyqsE2xBtEKzBBggU01JTbGhNpUGiBgLElvDq9905s+/s7Ny7d/t2t/vKfsnJ3p0zM3fmzJkzZ87MbpL00UcfffTRRx999NFHH3300UcfffTRRx8dwZQpU94+a9as14XpxyvYV/Y5TB8tGDt27Fsqlcob/LOIjD9aQj0nhvV3Gsezvo123eomOiIrKPWFoOuPVwWLgX1N0/QG9HtuyOt1wACdirav9YqAPiwEHQG9gvRf4/PWJrQV9B8tQ9pdrVbHhe/pFOQ417fRrFvdRttlhYpOA/1x6tSp7wp5xzsmTpz4DvR9Cybz6SGvV4E2T+J4QQlO8Wnqrf2exgnpVyFpwBTJBfJXQYOgocmTJ38y5FtMmDDhTci3HvQq36PE560qx0uk3kiSnkF7zrH1SIG+If1s0HO+PMreN2nSpDd6/rRp097m+2locNy4cW+29fQCRptu6fjeATpsZMvxPWDSHgfNSQL9aqYbzINxnKZ11fjQufPIa5usqCxQmnugJPND3msF6Pv5nCQ0CiGvBzHA1QztXRwykFaBUuyloiDPuSG/AGNQ9GqUW4/nE0JmCCjdB5D3BdBmv+X14Hemg15CG2ZZHlFS3wbEeZGcRIdAZ4QZkDYXtNEau17EKNOtDGZ8NyZGH3Ts6EnREEUXvyLdAMYgfRFokMYtCYxiW2TFSvCCJ7q53eg1cNsGGTzUZJL1BKBI70Nb/8bPkEeAN48KB8XbgzGVkJ8HKivK3YpyU0JeCOT7gjjv6bshD/VMBG8f6BHwTwr5ZfSN5ZBvHT4v53tAq5JA+ZF2Bdth03oRo0m3PMQtFpT7FSEPY/IJ5cUMVq5u0ING/tVIvwxfx1ieRztkxdV+LSq5MWS81gAZLJVgRepFUFHylEmReXCqVGtbiVHZQ4YiUF/Exeo+FvKYRl6OTpXSN67yyLdC3IHFbnHblKrJcgK+/4L5TFrPYrTolkfR+IoarLz+aNmh1IQZoFPcOdyNRfhDNm8MpWTF1ZCWFZ+zrYJj3/tOFN4lzYNxA7rHrTsZ0xhI1Np2G1wBuB9P3Tan5TahPxeA9jM+FfK6jbzxorERt51bZPOH0JWOQf8joIUhfyRAu05CnY+A9tEbi/BpcPneBu+prL6xLPr6NT7jc7HWt8DzWY96cA0eoAdlQH3A53uTwLvjdxpDK9+YzL1OkYpidLGyFtIh3WI8kXVDHpP5ne9GWz+obcn1gIugp+UPgPZG6uBCsoHjEfOiYrqB5zksg/zvDvPHUCgrDiaY27XC+fi8BnSXjztw8oP+oUYghgGU+yzK/IudiNALVIywUJcxBu2/DG3Zgc+virPwDFw+jX6+J8ycB8oAZQ6UWUU6hWbjJW4h2U+FDcuGwLicjrz/JfE55B8tUN8M0L9BWziRtE0Z4T34kD8of0ZYtoS+ZUDZ5T6PbrHZj+3+RDd1XuDK+lIOemJ2FfOL8yaWgx5L3RY8m6CUH9KuBd0Pug3ffwC6WWX+EGg9nj+Fz41ax89Bu8NJ1my8PDqhW+pNrwYtYd14x5fwea/OAbbj+TQ4jCkDGR7fBi9JXDiDntvNOYa7VpaHBDoOr8SMXx5yZUVDA8ZzqOxqfM1WKFj0sUjbHAzswTQeN+H2gA16EZ9f4WBy1RQXn6EBGa+nF+Hq1034Nh7QIKMPXN6HtF1cycMCeWB/pISxgAw+l7pJWYpQ56O+bUUoOV4c7GfTyFYgBuT7orjV9E/tuucjw1uOv0jjVZBNoCHJiZ9RtpKvbxlYDu1dZ8bOewVDSD+fCaqHDR5gMqwPTyFPhQmsj+0BPUAjQP0A62eUKdLWiDuJm+crSNXDTM2kVbnzkKOmG2XGy0NK6lYroCwoB20Hg/BbzRUe/76GGGczMNjP/oMelfqxzbw21PmZJGcHJMO6cW3qQgtcCIbS4KS6CBKTlQZ47xUTe9AB5AuuTHQAVMH2s5JaYQV454obxNppmXFH19i8xwpo2ynijvhrQWOjwHcwTa840Gu7CTKY7fOFoAxUFrGJ0lG0MF6lPBwPTkhxyphN0KQoLlESUhBfYRp5zBPyiCJ98+AERb4ViRknTl5xhnKDyioaP6NcwHupYk6AVY48+c3apF7lNbot3wbaZL0N7V/ddlon+SHf57Lj5SEldEvHahVoe+gJxoA+fl2NGQP4lPnZnifDnlJDUL8ZZDgGdrG2O6MyzouW5QL6V7ZBZUwPNnpSHYO+r15W2lFa7cOcAFrpMp0ItUblKVhFYzVpYFlTNRbgr7P5jxWouBIEIH3fuXpRMcD7IfvA7See94D3eVuHhxFky0owUrQwXi0ZNAJ1i/b7SUzMCSG/Ffjxl3h8pebdSM7EzdM3C5bl2Nk0MzG49ZxD/WNbbB5CIsaWz0g7FF4zEJ309l1mwa7bbmm9tYvHZcfLQ0roljG8r6JNZ4X8PGjb6sYjNMBlYfp/oIxRtTC6sd/H9Ah8XyBOJxpOqmOIygqVnyNuohe6nHkKZiqtCz5LRAmOIXhXiZcA67Yw4tzel7kH1/7V4nziPLWtsQCv7zPLhDwLNfa1lasZUdFi8QaLFsarZYNGY4A231ltQxzNTOSG+Aq/M11y4mdEnr4ZcExXNsRPkqxstn0GPYn+/DTkm8lYF2oQd72joU2ink1qJj3zMK8dBzU0PGm1u4BS4+UhJXWLoQnV1aYTn5g+ffpbxXmZMQMcXXSK4PsvxafoUeTphriL2/Rkw5PqKKKyokIg8eWYABmsS3QPzMFEvmf8hPeIVurSaW2fshZYwQtzDBgOpi44+dFkuFMDqH820n4F+hFXH+/1Ie9MfF+N9FU8SUL6ycNVOiUtcnW5UqPsNg6sJvlrAZlS03Dh+5neoIgb6OhgId8UygJ0QcizqDqP59NlCe+6KOxXiBbGq1QbPXQbs1JMjGgkkJw7RgS3aFJw/4xIc/TNg+U4prHYpzEsR9iOkG8Mml2wvJHdxjHA5zIY+JQMiUz6mGdTcdvdw/pZpTFFXR+WEuPl0eq4lYVEtpY5XuYY3kiI6b2F6P2z2Pg2gwzHz0IvlIsUt9Lk1U6q8xCVlXHRl5q8fuJs8D9FECeQhp+m0OCkLrBea0DVnZrRyoZH7lQaxg6Yl43nTeCaMPE8D3XdwsmlSvE4FVbrG9TjdZ5kUVlrq6heE3lCCvbfqO9S8PZqvGOg4k5kGeTN4mc2r8ZOdoZbDw+VDV3+qHfRSZQdL8oN33dVynnIHIsrSXwOmUcBv1jkxc8yr0UK4quUrUT0zUPcEf+gGoUGVFyIocHbUviJkwX/mSB6yVgXvir1kJO6Fc8G368XXSBR9tt4nlt2vIL0tusW2yI5XqYYwyLuJ2lH2Lak0bP28PIbyhufAjTTjSy2Cvk9HMomRK6sUPhUJO4A3SUuMLwdFa+w2y1vzWMTRMvzpOOXWgcV4LQwH9LOYEO9EohTikyYakR4nSLrpDgrvvFEgO/ld6aTD3owNSu7tm2LuEnCSdkAPaL/Cfj/xOee1P0om4Kr64/muwF0cZIzubVttcnQbZQZr2RYcaJBdwtxyr6o2XbXInaxVr1cyvVFcZOCxNgR46snp84jD6/1HBQTpPbI07fUGUNbPxexC20eggsf32v1xEIPgDJDhTy/Vd34HujvoN9xEWW+qrtewsX5El+W/RR3d69u0qdum/w0aBDtXuLlWXK8MkiHdKviDHzdFjvmZWof/ifOUDTEy8G/Xerlz5sMD8Y8ZYtmusE8Fed0HLZ80PfDujykiawyV7PoAqwKJRYTIbyrmvvXMqkLBGcekTdCXpjiLsnt9ILB841U5qo7yWKgOrPCTCPPVFuDKntTV5VQYdT9hlC3XfQa+YNaep+8mxb2lV4m7xItDtK7jTLjNR/0cN6kJtCXueDf0IoxUznd5MekU2iib+1AJkM7Iai/wQThwkD51ck4ZtBNemwONB2vpIO6xbaGE19cnLghfsa8SF8ZpvcYRi4rBiJRyQ6ufiGvDGjQSHzWrSNdcXpt82nY8LyFQtct5DampW6fzAuM43UibZJIXIRI3b2ihi0n0s9kx3n3R5MyYfD95s4V3ehvpm6FYoB+esXcG/JQGWznp03vRagc/1zRO1kh6IWAd2eL984GKm67fndsQrcTI9W30YZu6lZO/CyDuOD8dWF6L6FtshL3X1rLk5ztWBHw8vdTgJgIl8Jw3CLObV8GmqHGisfZa8Xd28niZ4kzNAtQ5jfi/nJkH/OHddOTQ/oaxvRsutkePEsjlbgJ+WV8f15MjA9pZ0n935mQwp8NsewSGk4+B7yehG4rGm6kV92BxT38tOlF0J/P8OCA94caQg+dgIxA30YZuqpb6igcjIwjD+2uS1v7B5Zuo32yasNp2Bh/0si6Zs6c+fowg+jfvSTBCqFBwPvTyBaKnlXefRiUO0+cl8cViXG6tWrcWgLq+Tjb1aJHc6zREPBn+1N3kLM0jZy2WkKeb4BuA+2UYUOfF2xvO9qgb6MC3dYteu3ifvJUd60HafxH4ouSkRqKDqLtslKv58dpwc9SWoFujR6jkPVUaDM9C25p8LwJ7/mWBuvp1S0My3cDPB3Fu5e2TYhdhBqF70CeH+F3yPHyNPLTqxbodo5N+J5Ood361ms4Rro1EHMmeh3NZPV/5xBsz5TK2vcAAAAASUVORK5CYII=>