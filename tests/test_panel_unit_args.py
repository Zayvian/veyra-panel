"""Ensure optional empty flags survive the actual installer unit template."""
import os
from pathlib import Path
import shlex
import subprocess
import unittest

INSTALLER = Path(__file__).resolve().parents[1] / "deploy/install-panel.sh"


class PanelUnitArguments(unittest.TestCase):
    def test_optional_values_do_not_consume_next_flag(self):
        source = INSTALLER.read_text()
        template = next(line for line in source.splitlines() if line.startswith("ExecStart="))
        for email in ("", "admin@example.com"):
            for sub in ("", "sub.example.com"):
                with self.subTest(email=email, sub=sub):
                    env = os.environ.copy()
                    env.update(ROOT="/opt/skysbx", DOMAIN="panel.example.com",
                               SUB_DOMAIN=sub, EMAIL=email)
                    rendered = subprocess.check_output(
                        ["bash", "-c", "cat <<EOF\n" + template + "\nEOF\n"], env=env, text=True)
                    args = shlex.split(rendered.removeprefix("ExecStart="))
                    self.assertEqual(args, ["/opt/skysbx/skysbx-panel", "--domain",
                        "panel.example.com", "--sub-domain=" + sub, "--acme-email=" + email,
                        "--db", "/opt/skysbx/skysbx.db"])


if __name__ == "__main__":
    unittest.main()
