"""Run on the server over SSH. Outputs only a two-minute, one-use URL."""
import argparse
import json
import urllib.request

from .browser import loopback_origin
from .smoke import read_config


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--origin', default='http://127.0.0.1:18010')
    args = parser.parse_args()
    if not loopback_origin(args.origin):
        parser.error('origin must be an HTTP loopback origin with a port')
    values = read_config('/etc/family-robot/livekit-dev.env')
    request = urllib.request.Request('http://127.0.0.1:18010/v1/browser/ticket',
        data=json.dumps({'origin': args.origin}).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + values['VOICE_CLIENT_TOKEN']})
    with urllib.request.urlopen(request, timeout=5) as response:
        ticket = json.load(response)['ticket']
    print(f'{args.origin}/client#pair={ticket}')


if __name__ == '__main__':
    main()
