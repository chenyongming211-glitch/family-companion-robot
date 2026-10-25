# DEPRECATED_PRODUCT_PATH (2026-10-25): historical ASR -> LLM -> TTS cascade.
# Current product voice core is Go internal/realtimelive; do not extend this path.
# Reference/legacy service only; this marker does not stop a running instance.
# See docs/CODE_LIFECYCLE.md before reusing any business logic.
import json
import logging
import sys


class MetadataHandler(logging.Handler):
    def emit(self, record):
        # Provider exception bodies and SDK tracebacks may contain conversation
        # text. Detailed stage/usage evidence is in the protected intent ledger.
        sys.stderr.write(json.dumps({'logger': record.name, 'level': record.levelname}) + '\n')


def configure_logging():
    root = logging.getLogger()
    root.handlers[:] = [MetadataHandler()]
    root.setLevel(logging.WARNING)
