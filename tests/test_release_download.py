"""fetch_release_binary: verified download, checksum refusal, and fallback."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

HELPER = Path(__file__).resolve().parents[1] / "deploy/release.sh"

# curl that serves files from $TEST_SERVE by the last path segment of the URL,
# and fails like a 404 for anything missing.
FAKE_CURL = '''#!/bin/sh
out=""; url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    --retry|--connect-timeout) shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
echo "$url" >> "$TEST_LOG"
name=${url##*/}
[ -f "$TEST_SERVE/$name" ] || exit 22
cp "$TEST_SERVE/$name" "$out"
'''

WORKING_BINARY = "#!/bin/sh\necho veyra-node test\n"


class ReleaseDownload(unittest.TestCase):
    def fetch(self, files, machine="x86_64"):
        sandbox = Path(tempfile.mkdtemp())
        serve, bins = sandbox / "serve", sandbox / "bin"
        serve.mkdir()
        bins.mkdir()
        for name, body in files.items():
            (serve / name).write_bytes(body.encode())
        (bins / "curl").write_text(FAKE_CURL)
        (bins / "uname").write_text("#!/bin/sh\necho " + machine + "\n")
        for tool in ("curl", "uname"):
            (bins / tool).chmod(0o700)
        log = sandbox / "log"
        log.write_text("")
        dest = sandbox / "installed"
        script = f'''
set -euo pipefail
say() {{ echo "say: $*"; }}
ok() {{ echo "ok: $*"; }}
warn() {{ echo "warn: $*"; }}
die() {{ echo "die: $*"; exit 3; }}
GH_OWNER=Zayvian
. "{HELPER}"
if fetch_release_binary veyra-node build-1 "{dest}"; then echo RESULT=installed; else echo RESULT=fallback; fi
'''
        env = dict(os.environ, TEST_SERVE=str(serve), TEST_LOG=str(log),
                   PATH=str(bins) + os.pathsep + os.environ["PATH"])
        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True,
                                text=True, timeout=15)
        return result, dest, log.read_text()

    @staticmethod
    def sums(**files):
        return "".join(f"{hashlib.sha256(body.encode()).hexdigest()}  {name}\n"
                       for name, body in files.items())

    def test_verified_download_is_installed(self):
        result, dest, log = self.fetch({
            "veyra-node-linux-amd64": WORKING_BINARY,
            "sha256sums.txt": self.sums(**{"veyra-node-linux-amd64": WORKING_BINARY}),
        })
        self.assertIn("RESULT=installed", result.stdout, result.stdout + result.stderr)
        self.assertEqual(dest.read_text(), WORKING_BINARY)
        self.assertTrue(os.access(dest, os.X_OK))
        self.assertIn("/Zayvian/veyra-panel/releases/download/build-1/veyra-node-linux-amd64", log)

    def test_arm64_asks_for_the_arm64_file(self):
        result, dest, log = self.fetch({
            "veyra-node-linux-arm64": WORKING_BINARY,
            "sha256sums.txt": self.sums(**{"veyra-node-linux-arm64": WORKING_BINARY}),
        }, machine="aarch64")
        self.assertIn("RESULT=installed", result.stdout, result.stdout + result.stderr)
        self.assertIn("veyra-node-linux-arm64", log)

    def test_checksum_mismatch_stops_the_install(self):
        result, dest, _ = self.fetch({
            "veyra-node-linux-amd64": WORKING_BINARY,
            "sha256sums.txt": self.sums(**{"veyra-node-linux-amd64": "something else"}),
        })
        self.assertEqual(result.returncode, 3, result.stdout + result.stderr)
        self.assertIn("does not match", result.stdout)
        self.assertFalse(dest.exists())

    def test_missing_checksum_line_stops_the_install(self):
        result, dest, _ = self.fetch({
            "veyra-node-linux-amd64": WORKING_BINARY,
            "sha256sums.txt": self.sums(**{"veyra-panel-linux-amd64": WORKING_BINARY}),
        })
        self.assertEqual(result.returncode, 3, result.stdout + result.stderr)
        self.assertFalse(dest.exists())

    def test_missing_file_falls_back_to_compiling(self):
        result, dest, _ = self.fetch({"sha256sums.txt": ""})
        self.assertIn("RESULT=fallback", result.stdout, result.stdout + result.stderr)
        self.assertFalse(dest.exists())

    def test_unknown_architecture_falls_back(self):
        result, dest, log = self.fetch({}, machine="riscv64")
        self.assertIn("RESULT=fallback", result.stdout)
        self.assertEqual(log, "")

    def test_binary_that_does_not_run_falls_back(self):
        broken = "#!/bin/sh\nexit 1\n"
        result, dest, _ = self.fetch({
            "veyra-node-linux-amd64": broken,
            "sha256sums.txt": self.sums(**{"veyra-node-linux-amd64": broken}),
        })
        self.assertIn("RESULT=fallback", result.stdout, result.stdout + result.stderr)
        self.assertFalse(dest.exists())


if __name__ == "__main__":
    unittest.main()
