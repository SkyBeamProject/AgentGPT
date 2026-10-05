#!/usr/bin/env python3
"""Write the Beam Agent icon and MSIX logo set. Uses only the standard library."""

import struct
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parent / "windows"
GREEN = (15, 110, 86, 255)
PAPER = (244, 241, 234, 255)
CLEAR = (0, 0, 0, 0)


def pixel(x, y, size):
    margin = max(1, size // 16)
    radius = max(2, size // 6)
    x0, y0 = margin, margin
    x1, y1 = size - 1 - margin, size - 1 - margin
    if x < x0 or y < y0 or x > x1 or y > y1:
        return CLEAR

    def outside(cx, cy):
        return (x - cx) ** 2 + (y - cy) ** 2 > radius ** 2

    if x < x0 + radius and y < y0 + radius and outside(x0 + radius, y0 + radius):
        return CLEAR
    if x > x1 - radius and y < y0 + radius and outside(x1 - radius, y0 + radius):
        return CLEAR
    if x < x0 + radius and y > y1 - radius and outside(x0 + radius, y1 - radius):
        return CLEAR
    if x > x1 - radius and y > y1 - radius and outside(x1 - radius, y1 - radius):
        return CLEAR
    if abs((x - x0) - (y - y0)) < max(1, size / 14):
        return PAPER
    return GREEN


def chunk(tag, data):
    return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)


def png(size):
    raw = bytearray()
    for y in range(size):
        raw.append(0)
        for x in range(size):
            raw.extend(pixel(x, y, size))
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(bytes(raw), 9)) + chunk(b"IEND", b"")


def ico(size):
    xor = bytearray()
    for y in range(size - 1, -1, -1):
        for x in range(size):
            r, g, b, a = pixel(x, y, size)
            xor.extend((b, g, r, a))
    row_bytes = ((size + 31) // 32) * 4
    mask = bytearray()
    for y in range(size - 1, -1, -1):
        row = bytearray(row_bytes)
        for x in range(size):
            if pixel(x, y, size)[3] < 128:
                row[x // 8] |= 1 << (7 - (x % 8))
        mask.extend(row)
    dib = struct.pack("<IiiHHIIiiII", 40, size, size * 2, 1, 32, 0, len(xor) + len(mask), 0, 0, 0, 0)
    image = dib + bytes(xor) + bytes(mask)
    entry = struct.pack("<BBBBHHII", size, size, 0, 0, 1, 32, len(image), 22)
    return struct.pack("<HHH", 0, 1, 1) + entry + image


def main():
    assets = ROOT / "assets"
    assets.mkdir(parents=True, exist_ok=True)
    (ROOT / "beam-agent.ico").write_bytes(ico(32))
    (assets / "Square44x44Logo.png").write_bytes(png(44))
    (assets / "Square150x150Logo.png").write_bytes(png(150))
    (assets / "StoreLogo.png").write_bytes(png(50))


if __name__ == "__main__":
    main()
