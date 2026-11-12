#!/usr/bin/env python3
"""Cloud-only preparation of the licensed Carefree A/B excerpt using FFmpeg."""
import argparse
import hashlib
import json
import math
import struct
import subprocess
import urllib.request
import wave
from pathlib import Path

TRACK_PAGE = 'https://incompetech.com/music/royalty-free/index.html?isrc=USUAN1400037'
SOURCE_URL = 'https://incompetech.com/music/royalty-free/mp3-royaltyfree/Carefree.mp3'
METADATA_URL = 'https://incompetech.com/music/royalty-free/pieces.json'
LICENSE_URL = 'https://creativecommons.org/licenses/by/4.0/'


def choose_gain(integrated, true_peak, target=-23, ceiling=-2):
    if not all(math.isfinite(v) for v in (integrated, true_peak, target, ceiling)):
        raise ValueError('nonfinite loudness measurement')
    return min(target - integrated, ceiling - true_peak)


def run(arguments):
    return subprocess.run(arguments, check=True, capture_output=True, text=True)


def ffmpeg(*arguments):
    return run(['ffmpeg', '-hide_banner', '-nostdin', '-y', *map(str, arguments)])


def measure(path):
    result = ffmpeg('-i', path, '-af', 'loudnorm=I=-23:TP=-2:LRA=7:print_format=json', '-f', 'null', '-')
    data = result.stderr[result.stderr.rfind('{'):]
    parsed = json.loads(data)
    values = {key: float(parsed[field]) for key, field in
              (('integrated_lufs', 'input_i'), ('true_peak_dbtp', 'input_tp'), ('lra_lu', 'input_lra'))}
    if not all(math.isfinite(v) for v in values.values()):
        raise ValueError('nonfinite loudness results')
    return values


def download(url, path, limit=20*1024*1024):
    request = urllib.request.Request(url, headers={'User-Agent': 'FamilyRobotMusicAudition/1.0'})
    with urllib.request.urlopen(request, timeout=60) as response, path.open('wb') as output:
        if not response.url.startswith('https://'):
            raise ValueError('download lost HTTPS')
        size = 0
        while True:
            block = response.read(65536)
            if not block:
                break
            size += len(block)
            if size > limit:
                raise ValueError('download exceeds bounded asset size')
            output.write(block)
        declared = response.headers.get('Content-Length')
        if declared is not None and int(declared) != size:
            raise ValueError('incomplete HTTP download')
        return {'requested_url': url, 'final_url': response.url,
                'content_type': response.headers.get('Content-Type'), 'bytes': size}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def pcm_info(path):
    peak = count = 0
    with wave.open(str(path), 'rb') as audio:
        if audio.getnchannels() != 1 or audio.getsampwidth() != 2:
            raise ValueError('invalid prepared format')
        rate = audio.getframerate()
        while True:
            block = audio.readframes(1440)
            if not block:
                break
            values = struct.unpack('<' + 'h'*(len(block)//2), block)
            peak = max(peak, max(abs(v) for v in values))
            count += len(values)
    if peak >= 32767:
        raise ValueError('clipped final integer PCM')
    return {'sample_rate': rate, 'samples': count, 'sample_peak': peak,
            'sha256': sha(path), 'bytes': path.stat().st_size}


def prepare(output, start=30, seconds=30):
    if not math.isfinite(start) or not math.isfinite(seconds) or start < 0 or not 3 <= seconds <= 120:
        raise ValueError('excerpt must be 3..120 seconds at a finite nonnegative offset')
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        raise ValueError('use an empty output directory; preserve earlier evidence')
    downloads = {}
    for name, url in (('Carefree.mp3', SOURCE_URL), ('track-page.html', TRACK_PAGE),
                      ('pieces.json', METADATA_URL), ('license.html', LICENSE_URL)):
        downloads[name] = download(url, output/name)
        downloads[name]['sha256'] = sha(output/name)
    metadata = json.loads((output/'pieces.json').read_text())
    # Preserve the official metadata row, independently of the filename.
    rows = metadata.values() if isinstance(metadata, dict) else metadata
    matches = [row for row in rows if isinstance(row, dict) and
               'USUAN1400037' in json.dumps(row) and 'Carefree' in json.dumps(row)]
    if len(matches) != 1:
        raise ValueError('official Carefree/ISRC metadata mismatch')
    (output/'track-metadata.json').write_text(json.dumps(matches[0], indent=2)+'\n')
    source = output/'Carefree.mp3'
    probe = json.loads(run(['ffprobe','-v','error','-show_streams','-show_format','-of','json',str(source)]).stdout)
    streams = [s for s in probe['streams'] if s['codec_type'] == 'audio']
    if len(streams) != 1 or streams[0]['channels'] != 2 or float(probe['format']['duration']) < start+seconds:
        raise ValueError('source is not expected stereo or too short')
    (output/'source-probe.json').write_text(json.dumps(probe,indent=2)+'\n')
    mono = output/'mono-excerpt-float.wav'
    ffmpeg('-i', source, '-ss', start, '-t', seconds, '-vn', '-af',
           'pan=mono|c0=0.5*c0+0.5*c1', '-c:a', 'pcm_f32le', mono)
    initial = measure(mono)
    gain = choose_gain(initial['integrated_lufs'], initial['true_peak_dbtp'])
    master = output/'shared-gain-float.wav'
    ffmpeg('-i', mono, '-af', f'volume={gain:.8f}dB', '-c:a', 'pcm_f32le', master)
    results = {}
    for rate in (24000, 48000):
        path = output/f'carefree-{rate}.wav'
        ffmpeg('-i', master, '-ar', rate, '-ac', 1, '-c:a', 'pcm_s16le', path)
        info = pcm_info(path)
        info.update(measure(path))
        if info['true_peak_dbtp'] >= -0.1:
            raise ValueError('final resampling true peak lacks clipping headroom')
        if abs(info['samples'] - seconds*rate) > 1:
            raise ValueError('final sample count differs from requested excerpt')
        info['path'] = str(path.resolve())
        results[str(rate)] = info
    attribution = ('"Carefree" Kevin MacLeod (incompetech.com)\n'
                   'Licensed under Creative Commons: By Attribution 4.0 License\n'
                   + LICENSE_URL + '\nSource: ' + TRACK_PAGE + '\n'
                   + f'Changes: excerpt {start}s..{start+seconds}s; stereo averaged to mono once; '
                   + f'shared static gain {gain:.8f} dB once; separately resampled to 24/48kHz PCM16 mono.\n'
                   + 'Device volume must remain unchanged; equal sound pressure and improved quality are not established.\n')
    (output/'ATTRIBUTION.txt').write_text(attribution)
    report = {'title':'Carefree','author':'Kevin MacLeod','isrc':'USUAN1400037',
              'license':LICENSE_URL,'downloads':downloads,'start_seconds':start,'seconds':seconds,
              'mono_mix':'0.5*left+0.5*right','shared_gain_db':gain,'target_lufs':-23,
              'shared_true_peak_ceiling_dbtp':-2,'initial_mono':initial,
              'master_sha256':sha(master),'ffmpeg_version':run(['ffmpeg','-version']).stdout.splitlines()[0],
              'outputs':results,'physical_loudness_equal':False,'subjective_improvement_verified':False}
    (output/'manifest.json').write_text(json.dumps(report,indent=2)+'\n')
    return report


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--start',type=float,default=30)
    parser.add_argument('--seconds',type=float,default=30)
    args=parser.parse_args()
    report=prepare(args.output,args.start,args.seconds)
    print(json.dumps({'manifest':str((args.output/'manifest.json').resolve()),
                      'shared_gain_db':report['shared_gain_db'],'outputs':report['outputs']},indent=2))

if __name__ == '__main__':
    main()
