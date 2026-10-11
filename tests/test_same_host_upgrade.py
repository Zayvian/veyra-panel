"""Exercise the real combined launcher with isolated fake installers/downloads."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

LAUNCHER = Path(__file__).resolve().parents[1] / "install-panel-and-node.sh"

# Stands in for `git clone ... <destination>`: lays out a checkout of the
# repository holding just the two component installers under test.
FAKE_GIT = '''#!/bin/sh
set -eu
for arg in "$@"; do destination=$arg; done
mkdir -p "$destination/panel/deploy" "$destination/node/deploy"
cp "$TEST_FIXTURES/panel.sh" "$destination/panel/deploy/install-panel.sh"
cp "$TEST_FIXTURES/node.sh" "$destination/node/deploy/install-node.sh"
echo "$@" >> "$TEST_LOG"
'''

PANEL_FIXTURE = '''#!/bin/bash
set -eu
domain=$(sed -n 's/^SKYSBX_DOMAIN=//p' "$SKYSBX_ROOT/panel.env")
while [ "$#" -gt 0 ]; do
  case "$1" in
    --upgrade) shift ;;
    --src) [ -f "$2/node/deploy/install-node.sh" ]; shift 2 ;;
    --release) echo "panel release $2" >> "$TEST_LOG"; shift 2 ;;
    --domain) domain=$2; shift 2 ;;
    *) exit 12 ;;
  esac
done
printf 'SKYSBX_DOMAIN=%s\\n' "$domain" > "$SKYSBX_ROOT/panel.env"
mkdir -p "$SKYSBX_ROOT/certs/certificates/test"
touch "$SKYSBX_ROOT/certs/certificates/test/$domain.crt"
touch "$SKYSBX_ROOT/certs/certificates/test/$domain.key"
'''

NODE_FIXTURE = '''#!/bin/bash
set -eu
upgrade=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --upgrade) upgrade=1; shift ;;
    --src) [ -f "$2/panel/deploy/install-panel.sh" ]; shift 2 ;;
    --release) echo "node release $2" >> "$TEST_LOG"; shift 2 ;;
    *) exit 13 ;;
  esac
done
[ "$upgrade" = 1 ]
[ -L "$SKYSBX_ROOT/cert.pem" ]
[ -f "$SKYSBX_ROOT/cert.pem" ]
[ -f "$SKYSBX_ROOT/key.pem" ]
touch "$SKYSBX_ROOT/node-upgraded"
'''


def fake_curl(latest):
    """curl that answers the /releases/latest lookup, or fails like no network."""
    if latest is None:
        return "#!/bin/sh\nexit 22\n"
    return ("#!/bin/sh\nprintf 'https://github.com/Zayvian/veyra-panel/releases/tag/%s' "
            + latest + "\n")


class SameHostUpgrade(unittest.TestCase):
    def run_launcher(self, explicit, latest):
        directory = tempfile.mkdtemp()
        sandbox = Path(directory)
        root, bins, fixtures = [sandbox / name for name in ("data", "bin", "fixtures")]
        for path in (root, bins, fixtures):
            path.mkdir()
        log = sandbox / "log"
        log.write_text("")
        (root / "panel.env").write_text("SKYSBX_DOMAIN=panel.example.com\n")
        (root / "node.env").write_text("SKYSBX_PANEL=https://panel.example.com\nSKYSBX_TOKEN=test\n")
        scripts = {
            bins / "id": "#!/bin/sh\necho 0\n",
            bins / "docker": "#!/bin/sh\nexit 0\n",
            bins / "git": FAKE_GIT,
            bins / "curl": fake_curl(latest),
            fixtures / "panel.sh": PANEL_FIXTURE,
            fixtures / "node.sh": NODE_FIXTURE,
        }
        for path, body in scripts.items():
            path.write_text(body)
            path.chmod(0o700)
        env = {k: v for k, v in os.environ.items() if not k.startswith(("VEYRA_", "SKYSBX_"))}
        env.update(SKYSBX_ROOT=str(root), TEST_FIXTURES=str(fixtures), TEST_LOG=str(log),
                   PATH=str(bins) + os.pathsep + env["PATH"])
        args = ["sh", str(LAUNCHER), "--upgrade"]
        if explicit:
            args += ["--domain", "new.example.com"]
        result = subprocess.run(args, env=env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        domain = "new.example.com" if explicit else "panel.example.com"
        self.assertEqual((root / "cert.pem").resolve().name, domain + ".crt")
        self.assertEqual((root / "key.pem").resolve().name, domain + ".key")
        self.assertTrue((root / "node-upgraded").exists())
        return log.read_text()

    def test_saved_and_explicit_domain(self):
        for explicit in (False, True):
            with self.subTest(explicit=explicit):
                self.run_launcher(explicit, latest=None)

    def test_both_components_use_the_latest_release(self):
        log = self.run_launcher(False, latest="build-20261011-abc1234")
        # One clone, at the release tag, and both installers told to use it.
        self.assertEqual(log.count("clone"), 1, log)
        self.assertIn("--branch build-20261011-abc1234", log)
        self.assertIn("panel release build-20261011-abc1234", log)
        self.assertIn("node release build-20261011-abc1234", log)

    def test_no_release_falls_back_to_main(self):
        log = self.run_launcher(False, latest=None)
        self.assertIn("--branch main", log)
        self.assertNotIn("release", log)


if __name__ == "__main__":
    unittest.main()
