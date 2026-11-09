"""FRC1 screen supply encoding. Pillow is loaded only for image conversion."""
import io
import re
import struct
import zlib

WIDTH = HEIGHT = 280
SIZE = 9848

def parse(data, device_id):
    if (len(data) != SIZE or not re.fullmatch(r'dev_[0-9a-f]{32}', device_id)
            or data[:8] != b'FRC1' + struct.pack('<HH', WIDTH, HEIGHT)
            or data[8:44] != device_id.encode('ascii')
            or struct.unpack('<I', data[44:48])[0] != zlib.crc32(data[48:])):
        raise ValueError('invalid screen code')
    return data[48:]

def encode(data, device_id):
    from PIL import Image
    with Image.open(io.BytesIO(data)) as im:
        if im.format not in ('PNG', 'JPEG') or im.size != (WIDTH, HEIGHT) or getattr(im, 'n_frames', 1) != 1:
            raise ValueError('invalid screen image')
        im.load()
        # Composite any alpha onto white; do not resize or dither code edges.
        rgba = im.convert('RGBA')
        white = Image.new('RGBA', im.size, 'white')
        gray = Image.alpha_composite(white, rgba).convert('L')
        payload = bytearray(9800)
        for y in range(HEIGHT):
            for x in range(WIDTH):
                if gray.getpixel((x,y)) < 128:
                    payload[y*35+x//8] |= 0x80 >> (x%8)
    result = b'FRC1'+struct.pack('<HH', WIDTH, HEIGHT)+device_id.encode('ascii')+struct.pack('<I',zlib.crc32(payload))+payload
    parse(result, device_id)
    return result
