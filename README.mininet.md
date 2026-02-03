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

Mini‑NDN 在清理阶段会调用 `nfd-stop`，因此即使你使用的是 Go 版 `ndnd`，也需要安装 NFD 工具链或提供同名脚本。

### 方式 A：使用 NDN PPA（推荐，若可用）

```bash
sudo apt install software-properties-common
sudo add-apt-repository ppa:named-data/ppa
sudo apt update
sudo apt install nfd
```

如果 `add-apt-repository` 报错类似：

```
E: The repository 'https://ppa.launchpadcontent.net/named-data/ppa/ubuntu noble Release' does not have a Release file.
```

说明当前 Ubuntu 版本暂无该 PPA，请使用方式 B（源码安装）。

### 方式 B：从源码安装（PPA 不可用时）

安装依赖：

```bash
sudo apt install build-essential pkg-config libboost-all-dev \
  libsqlite3-dev libssl-dev libpcap-dev libsystemd-dev
```

编译并安装 `ndn-cxx` 和 `NFD`：

```bash
git clone https://github.com/named-data/ndn-cxx.git
git clone --recursive https://github.com/named-data/NFD.git

cd ndn-cxx
./waf configure --prefix=/usr --sysconfdir=/etc
./waf
sudo ./waf install

cd ../NFD
./waf configure --prefix=/usr --sysconfdir=/etc
./waf
sudo ./waf install
```

安装完成后应能找到 `nfd-start` / `nfd-stop`。

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

## 常见问题排查

- **Go build 缓存权限错误**
  - 现象：`/root/.cache/go-build` 下 `permission denied`
  - 解决：指定可写缓存目录：
    ```bash
    make GOCACHE=/tmp/go-build
    ```

- **`sudo` 失败或要求输入密码**
  - Mininet 需要 root。确保环境允许 `sudo -E`。
  - 若无法使用 `sudo`，则无法运行基于 Mininet 的流程。

## 备注

- 本流程适用于 Go 版本 `ndnd`，不是 C++ 的 NFD。
- 如果之后修改了 Go 源码，需要重新编译并按上述流程再次运行。
