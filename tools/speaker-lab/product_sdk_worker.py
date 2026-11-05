#!/usr/bin/env python3
"""Research-only official SDK adapter. Cloud only; never a production voice service.

Bundle v1: {schema_version,profile,assets:[{role,path,sha256}],metadata}.
Metadata and the complete role/hash set must exactly match products.lock.json.
Every asset is a canonical regular file, authenticated before any SDK import/load.
The bundle SHA is the existing protocol's model_sha256, not a weights digest.
"""
import argparse
import array
import hashlib
import importlib.util
import io
import json
import math
import numbers
import os
from pathlib import Path
import re
import signal
import socket
import stat
import struct
import sys
import tempfile
import time
import wave

REQUEST_KEYS = set('version id session scope generation start end model_sha256 sample_rate pcm_bytes'.split())
RESPONSE_KEYS = set('version kind id session scope generation start end model_sha256 dimension vector_bytes inference_us code'.split())
TOKEN = re.compile(r'^[A-Za-z0-9_-]{1,64}$')
SHA = re.compile(r'^[0-9a-f]{64}$')


class Invalid(Exception):
    """Deliberately contains no paths, samples, vectors, or SDK messages."""


def clear(value):
    if value is not None:
        if hasattr(value, 'fill'):
            value.fill(0)
        elif isinstance(value, array.array):
            for i in range(len(value)):
                value[i] = 0
        else:
            value[:] = b'\0' * len(value)


def strict_json(raw):
    def pairs(items):
        result = {}
        for k, v in items:
            if k in result or v is None:
                raise Invalid()
            result[k] = v
        return result
    try:
        obj = json.loads(raw, object_pairs_hook=pairs,
                         parse_constant=lambda _: (_ for _ in ()).throw(Invalid()))
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise Invalid() from None
    if not isinstance(obj, dict):
        raise Invalid()
    return obj


def canonical(path):
    if not isinstance(path, str) or not path.startswith('/'):
        raise Invalid()
    p = Path(path)
    try:
        if str(p) != path or str(p.resolve(strict=True)) != path:
            raise Invalid()
        if not stat.S_ISREG(p.stat().st_mode):
            raise Invalid()
    except (OSError, RuntimeError):
        raise Invalid() from None
    return p


def load_bundle(path, digest, lock=None):
    p = canonical(path)
    if not isinstance(digest, str) or not SHA.fullmatch(digest):
        raise Invalid()
    with p.open('rb') as f:
        raw = f.read(65537)
    if len(raw) > 65536 or hashlib.sha256(raw).hexdigest() != digest:
        raise Invalid()
    obj = strict_json(raw)
    if set(obj) != {'schema_version', 'profile', 'assets', 'metadata'} or type(obj['schema_version']) is not int or obj['schema_version'] != 1:
        raise Invalid()
    if lock is None:
        lock = strict_json(Path(__file__).with_name('products.lock.json').read_bytes())
    profile = lock['profiles'].get(obj['profile']) if isinstance(obj['profile'], str) else None
    if profile is None or json.dumps(obj['metadata'], sort_keys=True) != json.dumps(profile['metadata'], sort_keys=True) or type(obj['assets']) is not list:
        raise Invalid()
    paths = {}
    # Authenticate the entire set before any SDK has an opportunity to load.
    for asset in obj['assets']:
        if not isinstance(asset, dict) or set(asset) != {'role', 'path', 'sha256'}:
            raise Invalid()
        role = asset['role']
        if not isinstance(role, str) or role in paths or role not in profile['assets'] or asset['sha256'] != profile['assets'][role]:
            raise Invalid()
        ap = canonical(asset['path'])
        h = hashlib.sha256()
        with ap.open('rb') as f:
            for block in iter(lambda: f.read(1048576), b''):
                h.update(block)
        if h.hexdigest() != asset['sha256']:
            raise Invalid()
        paths[role] = str(ap)
    if set(paths) != set(profile['assets']):
        raise Invalid()
    # SDK directory loaders must see only the authenticated immediate/subtree files.
    for prefix in ('sb/', 'asr/', 'spk/'):
        members = {k[len(prefix):]: v for k, v in paths.items() if k.startswith(prefix)}
        if members:
            roots = {str(Path(v).parents[len(Path(k).parts)-1]) for k, v in members.items()}
            if len(roots) != 1:
                raise Invalid()
            root = Path(next(iter(roots)))
            actual = {str(f.relative_to(root)) for f in root.rglob('*') if f.is_file() or f.is_symlink()}
            if actual != set(members):
                raise Invalid()
    return dict(profile=obj['profile'], metadata=obj['metadata'], paths=paths)


def exact_read(stream, size):
    out = bytearray(size)
    try:
        offset = 0
        while offset < size:
            count = stream.readinto(memoryview(out)[offset:])
            if count is None or count <= 0:
                raise Invalid()
            offset += count
        return out
    except BaseException:
        clear(out)
        raise


def read_header(stream):
    size = struct.unpack('>I', exact_read(stream, 4))[0]
    if not 0 < size <= 4096:
        raise Invalid()
    return strict_json(exact_read(stream, size))


def read_request(stream, digest):
    h = read_header(stream)
    if set(h) != REQUEST_KEYS:
        raise Invalid()
    for k in ('version','id','generation','start','end','sample_rate','pcm_bytes'):
        if type(h[k]) is not int or not 0 <= h[k] <= 2**64-1:
            raise Invalid()
    if (h['version'] != 1 or h['id'] == 0 or h['generation'] == 0 or
        any(not isinstance(h[k], str) or not TOKEN.fullmatch(h[k]) for k in ('session','scope')) or
        h['model_sha256'] != digest or not SHA.fullmatch(digest) or h['sample_rate'] != 16000 or
        not 0 < h['end']-h['start'] <= 128000 or
        h['pcm_bytes'] != 2*(h['end']-h['start'])):
        raise Invalid()
    return h, exact_read(stream, h['pcm_bytes'])


def vector(values, dimension):
    if type(dimension) is not int or not 1 <= dimension <= 1024 or len(values) != dimension:
        raise Invalid()
    result = array.array('f')
    try:
        for n in values:
            if isinstance(n, bool) or not isinstance(n, numbers.Real) or not math.isfinite(float(n)):
                raise Invalid()
            result.append(float(n))
        if not all(math.isfinite(x) for x in result) or not sum(float(x)*x for x in result) > 0:
            raise Invalid()
        return result
    except BaseException:
        clear(result)
        raise Invalid() from None


def write_frame(stream, h, payload=b''):
    raw = json.dumps(h, separators=(',', ':'), allow_nan=False).encode()
    if not 0 < len(raw) <= 4096:
        raise Invalid()
    for part in (struct.pack('>I', len(raw)), raw, payload):
        remaining = memoryview(part)
        while remaining:
            n = stream.write(remaining)
            if n is None or n <= 0:
                raise Invalid()
            remaining = remaining[n:]
    stream.flush()


def response(digest, dimension):
    return dict(version=1,kind='ready',id=0,session='',scope='',generation=0,start=0,end=0,
                model_sha256=digest,dimension=dimension,vector_bytes=0,inference_us=0,code='ok')


def write_ready(stream, digest, dimension):
    if not SHA.fullmatch(digest) or not 1 <= dimension <= 1024:
        raise Invalid()
    write_frame(stream, response(digest, dimension))


def write_result(stream, request, dimension, values, us):
    if type(us) is not int or us < 0 or type(dimension) is not int or not 1 <= dimension <= 1024:
        raise Invalid()
    h = response(request['model_sha256'], dimension)
    h.update({k: request[k] for k in ('id','session','scope','generation','start','end')})
    h.update(kind='result', inference_us=us, code='unavailable' if values is None else 'ok')
    owned = payload = None
    try:
        if values is not None:
            owned = vector(values, dimension)
            if sys.byteorder != 'little':
                owned.byteswap()
            payload = bytearray(memoryview(owned).cast('B'))
            h['vector_bytes'] = len(payload)
        write_frame(stream, h, payload or b'')
    finally:
        clear(owned)
        clear(payload)


def serve(reader, writer, digest, dimension, extract):
    write_ready(writer, digest, dimension)
    last = 0
    while True:
        pcm = values = None
        try:
            h, pcm = read_request(reader, digest)
            if h['id'] <= last:
                raise Invalid()
            last = h['id']
            started = time.monotonic_ns()
            values = extract(pcm)
            write_result(writer, h, dimension, values, (time.monotonic_ns()-started)//1000)
        finally:
            clear(pcm)
            clear(values)


def sdk_extractor(bundle):
    """Use pinned upstream frontends/native APIs; no homemade speaker algorithm."""
    import importlib.metadata
    import numpy as np
    paths, meta = bundle['paths'], bundle['metadata']
    if importlib.metadata.version(meta['sdk']) != meta['sdk_version']:
        raise Invalid()
    if bundle['profile'] == 'speechbrain_ecapa':
        import torch
        from speechbrain.inference.speaker import SpeakerRecognition
        torch.set_num_threads(1)
        torch.set_num_interop_threads(1)
        folder = str(Path(paths['sb/hyperparams.yaml']).parent)
        # Upstream collection creates label_encoder.ckpt; keep its links out of
        # the authenticated source directory and remove them after model loading.
        with tempfile.TemporaryDirectory(prefix='speaker-sdk-') as savedir:
            model = SpeakerRecognition.from_hparams(source=folder, savedir=savedir,
                        overrides={'pretrained_path': folder}, run_opts={'device':'cpu'})
        def extract(pcm):
            samples = np.frombuffer(pcm, dtype='<i2').astype(np.float32)
            samples /= 32768.0
            tensor = torch.from_numpy(samples).unsqueeze(0)
            output = None
            try:
                with torch.inference_mode():
                    output = model.encode_batch(tensor, normalize=False)
                return vector(output.detach().cpu().numpy().reshape(-1), meta['dimension'])
            finally:
                samples.fill(0)
                if output is not None:
                    with torch.inference_mode():
                        output.zero_()
        return extract
    if bundle['profile'] == 'wespeaker_cn34_ref':
        import torch
        import onnxruntime as ort
        # torchaudio 2.10 lazily imports its official decoder on first load.
        # Initialize that library before ready, within the existing 10s budget.
        from torchcodec.decoders import AudioDecoder  # noqa: F401
        torch.set_num_threads(1)
        torch.set_num_interop_threads(1)
        spec = importlib.util.spec_from_file_location('pinned_wespeaker_reference', paths['reference'])
        ref = importlib.util.module_from_spec(spec)
        # Execute the authenticated source, bypassing unbound __pycache__ files.
        exec(compile(spec.loader.get_source(spec.name), paths['reference'], 'exec'), ref.__dict__)
        opts = ort.SessionOptions()
        opts.inter_op_num_threads = opts.intra_op_num_threads = 1
        session = ort.InferenceSession(paths['onnx'], sess_options=opts, providers=['CPUExecutionProvider'])
        def extract(pcm):
            wav = io.BytesIO()
            feats = output = None
            try:
                with wave.open(wav, 'wb') as f:
                    f.setnchannels(1); f.setsampwidth(2); f.setframerate(16000); f.writeframes(pcm)
                wav.seek(0)
                feats = ref.compute_fbank(wav).unsqueeze(0).numpy()
                output = session.run(['embs'], {'feats':feats})[0]
                return vector(output.reshape(-1), meta['dimension'])
            finally:
                if feats is not None: feats.fill(0)
                if output is not None: output.fill(0)
                view = wav.getbuffer(); view[:] = b'\0'*len(view); view.release(); wav.close()
        return extract
    if bundle['profile'] == 'vosk':
        import vosk
        vosk.SetLogLevel(-1)
        model = vosk.Model(str(Path(paths['asr/am/final.mdl']).parents[1]))
        spk = vosk.SpkModel(str(Path(paths['spk/final.ext.raw']).parent))
        def extract(pcm):
            recognizer = vosk.KaldiRecognizer(model, 16000, spk)
            # CFFI borrows this mutable buffer; avoid an uncleared bytes copy.
            recognizer.AcceptWaveform(vosk._ffi.from_buffer('char[]', pcm))
            result = json.loads(recognizer.FinalResult())
            values = result.get('spk')
            try:
                return None if 'spk' not in result else vector(values, meta['dimension'])
            finally:
                if values is not None:
                    for i in range(len(values)): values[i] = 0.0
                result.clear()
        return extract
    raise Invalid()


def offline_audit(event, args):
    if event == 'socket.getaddrinfo' or (event == 'socket.connect' and args[0].family != socket.AF_UNIX):
        raise Invalid()


def stop_signal(signum, frame):
    # Python unwinds request finally blocks on TERM/INT; SIGKILL has no handler.
    raise SystemExit(1)


def main():
    # Silence native-library logging at the descriptor boundary, including failures.
    with open(os.devnull, 'wb') as null:
        os.dup2(null.fileno(), 1); os.dup2(null.fileno(), 2)
    signal.signal(signal.SIGTERM, stop_signal)
    signal.signal(signal.SIGINT, stop_signal)
    parser = argparse.ArgumentParser()
    parser.add_argument('--socket', required=True)
    parser.add_argument('--bundle', required=True)
    parser.add_argument('--sha256', required=True)
    args = parser.parse_args()
    # Minimal fixed environment: remove proxy, token, endpoint and auto-download inputs.
    keep = {k: os.environ[k] for k in ('PATH','VIRTUAL_ENV') if k in os.environ}
    os.environ.clear(); os.environ.update(keep)
    os.environ.update(OMP_NUM_THREADS='1',MKL_NUM_THREADS='1',OPENBLAS_NUM_THREADS='1',
                      HF_HUB_OFFLINE='1',HF_DATASETS_OFFLINE='1',TRANSFORMERS_OFFLINE='1',
                      HF_HUB_DISABLE_TELEMETRY='1',DO_NOT_TRACK='1')
    try:
        bundle = load_bundle(args.bundle, args.sha256)
        # Existing Go supervisor owns socket inode/privacy and timeouts.
        socket_path = Path(args.socket)
        if (not socket_path.is_absolute() or str(socket_path) != args.socket or
            str(socket_path.resolve(strict=True)) != args.socket or
            not stat.S_ISSOCK(socket_path.stat().st_mode)):
            raise Invalid()
        sys.addaudithook(offline_audit)
        extract = sdk_extractor(bundle)
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as conn:
            conn.connect(args.socket)
            with conn.makefile('rb', buffering=0) as reader, conn.makefile('wb', buffering=0) as writer:
                serve(reader, writer, args.sha256, bundle['metadata']['dimension'], extract)
    except BaseException:
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
