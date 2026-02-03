# NDNd + Mini-NDN/Mininet 本地流程（Go 版本）

本文记录在当前环境验证过的可用流程：编译 Go 版本 `ndnd` 并运行 Mini‑NDN 的 e2e 场景，在每个命名空间启动 `ndnd fw` 和 `ndnd dv`。

## 前置条件

- 已安装 Go 1.23+
- 已安装 Mininet + Mini‑NDN（Python）
- 可用的 `sudo`（Mininet 通常需要 root）

## 编译 `ndnd`

在仓库根目录执行：

```bash
cd ndnd

# 本环境下 Go 可能无法写默认缓存目录
# 使用可写的缓存目录
make GOCACHE=/tmp/go-build
```

编译产物为仓库根目录下的 `./ndnd`。

## 安装 NFD 工具链（提供 `nfd-stop`）

Mini‑NDN 清理阶段会调用 `nfd-stop`，因此即使使用 Go 版 `ndnd`，也需要安装 NFD 工具链或提供同名脚本。

建议优先走 NDN PPA；若当前系统版本没有 PPA 包，则按官方文档用源码安装 `ndn-cxx` + `NFD`。

## 确保使用的是新编译的二进制

e2e 脚本使用 `shutil.which(\"ndnd\")` 查找二进制，因此 PATH 必须优先指向你刚编译的 `ndnd`。

```bash
export PATH=$PWD:$PATH
which ndnd
ndnd -v
```

预期：`which ndnd` 指向仓库根目录的 `ndnd`，`ndnd -v` 显示当前版本。

## 修改源码后的继续使用

查看当前使用的 `ndnd` 版本（建议在 `ndnd/` 目录下执行）：

```bash
which ndnd
ndnd -v
```

修改源码后：

1. 重新编译：
   ```bash
   make GOCACHE=/tmp/go-build
   ```
2. 确认版本已更新：
   ```bash
   ndnd -v
   ```
3. 重新启动场景（运行中的 `ndnd` 进程不会自动切换到新二进制）：
   - e2e 自动测试：直接重新运行 `e2e/runner.py ...`
   - 手动测试：退出 Mininet CLI（`exit`）后重新运行 `manual/start_topo.py ...`

## 运行 Mini‑NDN e2e 场景

使用 `sudo -E` 保留 PATH，确保 Mininet 命名空间里找到的是新编译的 `ndnd`。

```bash
NDND_SKIP_NFD=1 sudo -E python3 e2e/runner.py e2e/topo.big.conf
```

这条命令会：
- 创建 Mininet 拓扑
- 在每个节点启动 `ndnd fw`
- 启动 `ndnd dv` 并等待路由收敛

说明：
- `NDND_SKIP_NFD=1`：跳过不需要的 NFD 场景（仅保留 ndnd 的 forwarder + dv）。
- `e2e/topo.big.conf`：示例大拓扑（8 个核心环 + 8 个叶子节点）。

## 常见问题排查（简版）

- Go build 缓存权限：用 `make GOCACHE=/tmp/go-build`
- 默认拓扑缺失：自建 `e2e/topo.min.conf` 后传入路径
- NFD 工具缺失：跳过 NFD 场景运行 `NDND_SKIP_NFD=1`
  - 大拓扑建议按需加大收敛等待：`NDND_CONVERGE_DEADLINE=300 ...`

## 手动测试：hello ndn

如果你希望启动大拓扑、等待路由收敛后手动执行 `put/cat`，可以使用脚本 `manual/start_topo.py`。

启动并进入 Mininet CLI：

```bash
sudo -E python3 ndnd/manual/start_topo.py ndnd/e2e/topo.big.conf
```

在 `mininet>` 里查看节点：

```text
mininet> nodes
```

例如选择 `n1` 作为 producer、`l1` 作为 consumer：

```text
mininet> n1 sh -c 'echo -n "hello ndn" | ndnd put --expose "/minindn/n1/hello" &'
mininet> l1 ndnd cat "/minindn/n1/hello"
```

预期输出：

```text
hello ndn
```

## 备注

- 本流程关注 `ndnd` 的 forwarder（`ndnd fw`）与路由（`ndnd dv`）。
