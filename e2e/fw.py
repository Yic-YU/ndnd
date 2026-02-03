import json
import os
import shutil

from minindn.apps.application import Application

class NDNd_FW(Application):
    def __init__(self, node, config={}, logLevel='INFO', threads=None):
        Application.__init__(self, node)

        if not shutil.which('ndnd'):
            raise Exception('ndnd not found in PATH, did you install it?')

        # 中文说明：日志文件名由 Mini-NDN 统一放在每个节点的 logDir 下（一般为 /tmp/minindn/<node>/log/）。
        # 在 Mininet CLI 中可用：
        #   <node> sh -c 'tail -n 200 -f /tmp/minindn/<node>/log/yanfd.log'
        self.logFile = 'yanfd.log'
        logLevel = node.params['params'].get('nfd-log-level', logLevel)

        # 中文说明：e2e 默认用单线程以简化 CS 审计/CSNAT 实验。
        # 也可通过环境变量覆盖：NDND_FW_THREADS=1
        if threads is None:
            try:
                threads = int(os.environ.get('NDND_FW_THREADS', '1'))
            except Exception:
                threads = 1
        threads = max(1, int(threads))

        self.confFile = f'{self.homeDir}/yanfd.json'
        self.ndnFolder = f'{self.homeDir}/.ndn'
        self.clientConf = f'{self.ndnFolder}/client.conf'
        self.sockFile = f'/run/nfd/{node.name}.sock'

        self.envDict = {
            'GOMAXPROCS': str(threads),
        }
        # 中文说明：把审计相关环境变量透传到 Mininet 节点内运行的 ndnd 进程。
        for k in ('NDND_CS_AUDIT_INTERVAL', 'NDND_CS_AUDIT_LOG'):
            v = os.environ.get(k)
            if v is not None:
                self.envDict[k] = v

        # Ensure the unix socket directory exists (shared FS, but required for binding).
        self.node.cmd('mkdir -p /run/nfd')
        self.node.cmd(f'rm -f {self.sockFile}')

        # Make default configuration
        default_config = {
            'core': {
                'log_level': logLevel,
            },
            'faces': {
                # dv.py uses udp4://<neighbor-ip>:6363, so enable unicast UDP listener explicitly.
                # Disable multicast to avoid unexpected multicast interface behavior in Mininet.
                'udp': {
                    'enabled_unicast': True,
                    'enabled_multicast': False,
                    'port_unicast': 6363,
                },
                'unix': {
                    'socket_path': self.sockFile,
                },
            },
            'fw': {
                'threads': threads,
            },
        }

        # Write YaNFD config file
        with open(self.confFile, "w") as f:
            json.dump(default_config | config, f, indent=4)

        # Create client configuration for host to ensure socket path is consistent
        # Suppress error if working directory exists from prior run
        os.makedirs(self.ndnFolder, exist_ok=True)

        # This will overwrite any existing client.conf files, which should not be an issue
        with open(self.clientConf, "w") as client_conf_file:
            client_conf_file.write(f"transport=unix://{self.sockFile}\n")

    def start(self):
        Application.start(self, f'ndnd fw run {self.confFile}',
                          logfile=self.logFile, envDict=self.envDict)
