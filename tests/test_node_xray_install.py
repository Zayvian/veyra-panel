"""install-node.sh's Xray step: download, reuse, and never a failed install."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT_DIR = Path(__file__).resolve().parents[1]
INSTALLER = ROOT_DIR / "node/deploy/install-node.sh"
LOCK = ROOT_DIR / "node/xray.lock"
VERSION = re.search(r"^XRAY_VERSION=(\S+)$", LOCK.read_text(), re.M).group(1)


def fake_xray(version):
    return f"#!/bin/sh\necho 'Xray {version} (Xray, Penetrates Everything.)'\necho second line\n"


def xray_step():
    """The installer's Xray section, from its banner to the next one."""
    text = INSTALLER.read_text()
    start = text.index("# ───────────────────────────────── xray")
    end = text.index("# ────────────────────────────── certificate")
    return text[start:end]


class XrayStep(unittest.TestCase):
    def run_step(self, release_file=None, installed=None, docker=False, from_source=0):
        sandbox = Path(tempfile.mkdtemp())
        root, build, bins = sandbox / "root", sandbox / "build", sandbox / "bin"
        (build / "veyra-node/node").mkdir(parents=True)
        root.mkdir()
        bins.mkdir()
        (build / "veyra-node/node/xray.lock").write_text(LOCK.read_text())
        if installed is not None:
            (root / "xray").write_text(installed)
            (root / "xray").chmod(0o755)
        if release_file is not None:
            (sandbox / "release-xray").write_text(release_file)
        log = sandbox / "log"
        if docker:
            # Stands in for the build: writes what a correct build would.
            (bins / "docker").write_text(
                f"#!/bin/sh\necho docker \"$@\" >> {log}\n"
                f"printf '%s' \"$FAKE_BUILT\" > {build}/xray\nchmod +x {build}/xray\n")
            (bins / "docker").chmod(0o755)
        script = f'''
set -euo pipefail
say() {{ echo "say: $*"; }}
ok() {{ echo "ok: $*"; }}
warn() {{ echo "warn: $*"; }}
die() {{ echo "die: $*"; exit 3; }}
fetch_release_binary() {{
    echo "fetch $1 $2" >> "{log}"
    [ -f "{sandbox}/release-xray" ] || return 1
    cp "{sandbox}/release-xray" "$3"
}}
ROOT="{root}"; BUILD="{build}"; FROM_SOURCE={from_source}; RELEASE=build-1
{xray_step()}
echo DONE
'''
        # Only the tools the step uses, so a Docker on the machine running the
        # tests does not count as one on the host being installed.
        for tool in ("sed", "cp", "chmod", "rm", "install", "cat", "sh", "printf", "echo"):
            found = shutil.which(tool)
            if found and not (bins / tool).exists():
                (bins / tool).symlink_to(found)
        env = dict(os.environ, FAKE_BUILT=fake_xray(VERSION), PATH=str(bins))
        result = subprocess.run([shutil.which("bash"), "-c", script], env=env, capture_output=True,
                                text=True, timeout=15)
        out = result.stdout + result.stderr
        self.assertIn("DONE", result.stdout, out)
        xray = root / "xray"
        return out, (xray.read_text() if xray.exists() else None), \
            (log.read_text() if log.exists() else "")

    def test_release_binary_is_installed(self):
        out, xray, _ = self.run_step(release_file=fake_xray(VERSION))
        self.assertEqual(xray, fake_xray(VERSION))
        self.assertIn(f"xray {VERSION} installed (build-1)", out)

    def test_current_xray_is_kept(self):
        out, xray, log = self.run_step(installed=fake_xray(VERSION))
        self.assertIn("already installed", out)
        self.assertNotIn("fetch", log)

    def test_old_xray_is_replaced_on_upgrade(self):
        out, xray, _ = self.run_step(installed=fake_xray("1.8.0"), release_file=fake_xray(VERSION))
        self.assertEqual(xray, fake_xray(VERSION))

    def test_nothing_to_download_and_no_docker_only_warns(self):
        out, xray, _ = self.run_step()
        self.assertIsNone(xray)
        self.assertIn("warn: no xray to download and no Docker", out)

    def test_wrong_version_from_release_falls_back_to_docker(self):
        out, xray, log = self.run_step(release_file=fake_xray("1.0.0"), docker=True)
        self.assertEqual(xray, fake_xray(VERSION))
        self.assertIn("deploy/build-xray.sh", log)
        self.assertIn("(built from source)", out)

    def test_from_source_builds_without_downloading(self):
        out, xray, log = self.run_step(release_file=fake_xray(VERSION), docker=True, from_source=1)
        self.assertNotIn("fetch", log)
        self.assertEqual(xray, fake_xray(VERSION))


if __name__ == "__main__":
    unittest.main()
