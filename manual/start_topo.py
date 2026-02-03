#!/usr/bin/env python3
"""Start a Mini-NDN topology with ndnd fw + ndnd dv, wait for convergence, then drop into Mininet CLI.

This is meant for manual experiments (e.g., run ndnd put/cat between chosen nodes).

Usage:
  sudo -E python3 ndnd/manual/start_topo.py ndnd/e2e/topo.big.conf

In Mininet CLI:
  mininet> nodes
  mininet> l1 ndnd fw route-list
  mininet> n1 ndnd put --expose "/minindn/n1/demo" < /tmp/x.bin &
  mininet> l1 ndnd cat "/minindn/n1/demo" > /tmp/y.bin
"""

import os
import sys

from mininet.cli import CLI
from mininet.log import setLogLevel, info

from minindn.minindn import Minindn
from minindn.apps.app_manager import AppManager

# Reuse the same app wrappers/utilities as the e2e tests.
E2E_DIR = os.path.join(os.path.dirname(__file__), '..', 'e2e')
sys.path.insert(0, os.path.abspath(E2E_DIR))

from fw import NDNd_FW  # noqa: E402
import dv_util  # noqa: E402


def main() -> int:
    setLogLevel('info')

    topo_file = sys.argv[1] if len(sys.argv) > 1 else os.path.join(E2E_DIR, 'topo.big.conf')
    topo_file = os.path.abspath(topo_file)

    # Ensure our repo-local ndnd is preferred when node.cmd('ndnd ...') is used.
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
    os.environ['PATH'] = repo_root + os.pathsep + os.environ.get('PATH', '')

    Minindn.cleanUp()
    Minindn.verifyDependencies()

    ndn = Minindn(topoFile=topo_file)
    ndn.start()

    try:
        info('Starting ndnd forwarder on nodes\n')
        AppManager(ndn, ndn.net.hosts, NDNd_FW)

        info('Starting ndnd dv on nodes and waiting for convergence\n')
        dv_util.setup(ndn)
        dv_util.converge(ndn.net.hosts)

        info('\nNetwork is up. Entering Mininet CLI.\n')
        info('Tip: run "nodes" to see hosts, then "<host> <command>" to run a command on a host.\n\n')
        CLI(ndn.net)
        return 0
    finally:
        # Best-effort cleanup.
        os.system('pkill -9 ndnd >/dev/null 2>&1 || true')
        ndn.stop()


if __name__ == '__main__':
    raise SystemExit(main())
