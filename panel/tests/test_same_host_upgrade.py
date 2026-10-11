"""Exercise the real combined launcher with isolated fake installers/downloads."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

LAUNCHER = Path(__file__).resolve().parents[1] / "install-panel-and-node.sh"


class SameHostUpgrade(unittest.TestCase):
    def test_saved_and_explicit_domain(self):
        for explicit in (False, True):
            with self.subTest(explicit=explicit), tempfile.TemporaryDirectory() as directory:
                sandbox = Path(directory)
                root, bins, fixtures = [sandbox / name for name in ("data", "bin", "fixtures")]
                for path in (root, bins, fixtures):
                    path.mkdir()
                (root / "panel.env").write_text("SKYSBX_DOMAIN=panel.example.com\n")
                (root / "node.env").write_text("SKYSBX_PANEL=https://panel.example.com\nSKYSBX_TOKEN=test\n")
                scripts = {
                    bins / "id": "#!/bin/sh\necho 0\n",
                    bins / "docker": "#!/bin/sh\nexit 0\n",
                    bins / "git": '''#!/bin/sh
set -eu
for arg in "$@"; do
  case "$arg" in
    */veyra-panel.git) component=panel ;;
    */veyra-node.git) component=node ;;
  esac
  destination=$arg
done
mkdir -p "$destination/deploy"
cp "$TEST_FIXTURES/$component.sh" "$destination/deploy/install-$component.sh"
''',
                    fixtures / "panel.sh": '''#!/bin/bash
set -eu
domain=$(sed -n 's/^SKYSBX_DOMAIN=//p' "$SKYSBX_ROOT/panel.env")
while [ "$#" -gt 0 ]; do
  case "$1" in
    --upgrade) shift ;;
    --domain) domain=$2; shift 2 ;;
    *) exit 12 ;;
  esac
done
printf 'SKYSBX_DOMAIN=%s\n' "$domain" > "$SKYSBX_ROOT/panel.env"
mkdir -p "$SKYSBX_ROOT/certs/certificates/test"
touch "$SKYSBX_ROOT/certs/certificates/test/$domain.crt"
touch "$SKYSBX_ROOT/certs/certificates/test/$domain.key"
''',
                    fixtures / "node.sh": '''#!/bin/bash
set -eu
[ "$1" = --upgrade ]
[ -L "$SKYSBX_ROOT/cert.pem" ]
[ -f "$SKYSBX_ROOT/cert.pem" ]
[ -f "$SKYSBX_ROOT/key.pem" ]
touch "$SKYSBX_ROOT/node-upgraded"
''',
                }
                for path, body in scripts.items():
                    path.write_text(body)
                    path.chmod(0o700)
                env = os.environ.copy()
                env.update(SKYSBX_ROOT=str(root), TEST_FIXTURES=str(fixtures),
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


if __name__ == "__main__":
    unittest.main()
