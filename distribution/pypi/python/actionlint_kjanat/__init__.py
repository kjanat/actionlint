import os
import subprocess
import sys


def main() -> None:
    name = "actionlint.exe" if os.name == "nt" else "actionlint"
    binary = os.path.join(os.path.dirname(__file__), "bin", name)
    args = [binary, *sys.argv[1:]]
    if os.name == "nt":
        sys.exit(subprocess.call(args))
    os.execv(binary, args)
