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

## 运行 Mini‑NDN e2e 场景

使用 `sudo -E` 保留 PATH，确保 Mininet 命名空间里找到的是新编译的 `ndnd`。

```bash
sudo -E python3 e2e/runner.py
```

这条命令会：
- 创建 Mininet 拓扑
- 在每个节点启动 `ndnd fw`
- 启动 `ndnd dv` 并等待路由收敛

## 常见问题排查（简版）

- Go build 缓存权限：用 `make GOCACHE=/tmp/go-build`
- 默认拓扑缺失：自建 `e2e/topo.min.conf` 后传入路径
- NFD 工具缺失：跳过 NFD 场景运行 `NDND_SKIP_NFD=1`

## 备注

- 本流程适用于 Go 版本 `ndnd`，不是 C++ 的 NFD。
- 如果之后修改了 Go 源码，需要重新编译并按上述流程再次运行。
