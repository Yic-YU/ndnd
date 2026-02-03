import os
import shlex
import time

from mininet.log import info
from mininet.node import Node

from minindn.minindn import Minindn
from minindn.apps.app_manager import AppManager

from dv import NDNd_DV, DEFAULT_NETWORK

def _get_unix_sock(node: Node) -> str:
    # Prefer the node's client.conf so we follow whatever the forwarder configured.
    sock = node.cmd(r"sed -n 's/^transport=unix:\/\///p' ~/.ndn/client.conf 2>/dev/null | head -n 1").strip()
    if sock:
        return sock
    return f'/run/nfd/{node.name}.sock'

def wait_fw_sockets(nodes: list[Node], deadline=30) -> None:
    info('Waiting for forwarder unix sockets\n')
    start = time.time()
    pending = {node.name: (node, _get_unix_sock(node)) for node in nodes}
    while time.time() - start < deadline:
        ready = []
        for name, (node, sock) in pending.items():
            # -S checks unix domain socket.
            if node.cmd(f'test -S {shlex.quote(sock)} && echo ok || true').strip() == 'ok':
                ready.append(name)
        for name in ready:
            pending.pop(name, None)
        if not pending:
            return
        time.sleep(0.5)

    missing = ', '.join(f'{n}:{p}' for n, p in pending.items())
    raise Exception(f'Forwarder sockets not ready: {missing}')

def setup(ndn: Minindn, network=DEFAULT_NETWORK) -> None:
    # Wait for fw to start and create node-local unix sockets. On larger topologies
    # a fixed sleep may be insufficient and dv will fail to connect.
    wait_fw_sockets(ndn.net.hosts, deadline=30)

    NDNd_DV.init_trust()
    info('Starting ndn-dv on nodes\n')
    AppManager(ndn, ndn.net.hosts, NDNd_DV, network=network)

def converge(nodes: list[Node], deadline=120, network=DEFAULT_NETWORK, use_nfdc=False) -> int:
    info('Waiting for routing to converge\n')
    deadline = int(os.environ.get('NDND_CONVERGE_DEADLINE', str(deadline)))
    start = time.time()
    while time.time() - start < deadline:
        time.sleep(1)
        if is_converged(nodes, network=network, use_nfdc=use_nfdc):
            total = round(time.time() - start)
            info(f'Routing converged in {total} seconds\n')
            return total

    raise Exception('Routing did not converge')

def is_converged(nodes: list[Node], network=DEFAULT_NETWORK, use_nfdc=False) -> bool:
    converged = True
    for node in nodes:
        if use_nfdc:
            # NFD returns status datasets without a FinalBlockId.
            # We don't support that.
            routes = node.cmd('nfdc route list')
        else:
            routes = node.cmd('ndnd fw route-list')
        for other in nodes:
            # Some forwarders may not list a self-route in their route table output.
            # Convergence here means every node has routes to every *other* node.
            if other == node:
                continue
            if f'{network}/{other.name}' not in routes:
                info(f'Routing not converged on {node.name} for {other.name}\n')
                converged = False
                break # break out of inner loop
        if not converged:
            return False
    return converged
