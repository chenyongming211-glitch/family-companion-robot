#!/usr/bin/env python3
"""Repository entry point; prepared trees carry the self-contained script."""
from pathlib import Path
import runpy

if __name__ == '__main__':
    runpy.run_path(str(Path(__file__).resolve().parents[1] / 'overlays/scripts/family_c3_pcm_cues.py'), run_name='__main__')
