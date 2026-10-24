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
