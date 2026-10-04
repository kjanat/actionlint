import os
import sys

# Invoked with -I: only the verified wheel is added to Python's isolated path.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from pyflakes.api import main  # pyright: ignore[reportMissingModuleSource]

main()
