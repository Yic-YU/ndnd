import random
import os
import time
import sys

from types import FunctionType

from mininet.log import setLogLevel, info
from minindn.minindn import Minindn

import test_001
import test_002

def run(scenario: FunctionType, **kwargs) -> None:
    try:
        random.seed(0)

        info(f"===================================================\n")
        start = time.time()
        scenario(ndn, **kwargs)
        info(f'Scenario completed in: {time.time()-start:.2f}s\n')
        info(f"===================================================\n\n")

        # Call all cleanups without stopping the network
        # This ensures we don't recreate the network for each test
        for cleanup in reversed(ndn.cleanups):
            cleanup()
    except Exception as e:
        ndn.stop()
        raise e
    finally:
        # kill everything we started just in case ...
        os.system('pkill -9 ndnd')
        os.system('pkill -9 nfd')

if __name__ == '__main__':
    setLogLevel('info')

    Minindn.cleanUp()
    Minindn.verifyDependencies()

    # 允许从命令行指定 topo 文件，和 README.mininet.md 的用法一致：
    #   sudo -E python3 e2e/runner.py e2e/topo.big.conf
    topo_file = sys.argv[1] if len(sys.argv) > 1 else None

    # 确保使用仓库内刚编译的 ndnd（二进制在 ndnd/ 目录下）。
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
    os.environ['PATH'] = repo_root + os.pathsep + os.environ.get('PATH', '')

    if topo_file is None:
        ndn = Minindn()
    else:
        ndn = Minindn(topoFile=os.path.abspath(topo_file))
    ndn.start()

    run(test_001.scenario_ndnd_fw)
    if os.environ.get('NDND_SKIP_NFD', '0') == '1':
        info('Skipping NFD scenario (NDND_SKIP_NFD=1)\n')
    else:
        run(test_001.scenario_nfd)
    run(test_002.scenario)

    ndn.stop()
