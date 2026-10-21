import struct, sys, os, datetime

out = sys.argv[1]
os.makedirs(out, exist_ok=True)
EPOCH = datetime.datetime(1904, 1, 1, tzinfo=datetime.timezone.utc)

def box(t, payload):
    return struct.pack(">I4s", 8 + len(payload), t) + payload

def mvhd(secs):
    # version 0: ctime, mtime, timescale, duration, rate, volume, reserved, matrix, predefined, next_track
    p = struct.pack(">BBBB", 0, 0, 0, 0)
    p += struct.pack(">IIII", secs, secs, 1000, 1000)
    p += struct.pack(">IH", 0x00010000, 0x0100) + b"\0" * 10
    p += struct.pack(">9I", 0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000)
    p += b"\0" * 24 + struct.pack(">I", 2)
    return box(b"mvhd", p)

def secs(dt):
    return int((dt - EPOCH).total_seconds())

def media(brand, s):
    ftyp = box(b"ftyp", brand + struct.pack(">I", 0) + brand)
    return ftyp + box(b"moov", mvhd(s)) + box(b"mdat", b"\0" * 16)

d = datetime.datetime(2023, 1, 2, 12, 34, 56, tzinfo=datetime.timezone.utc)
open(f"{out}/synth.mp4", "wb").write(media(b"isom", secs(d)))
open(f"{out}/synth.3gp", "wb").write(media(b"3gp4", secs(d)))
open(f"{out}/synth.mov", "wb").write(media(b"qt  ", secs(d)))
open(f"{out}/synth-zero-date.mp4", "wb").write(media(b"isom", 0))
# Apple-style mov: moov/meta(hdlr mdta, keys, ilst) with com.apple.quicktime.creationdate (has offset)
def apple_mov(value):
    key = b"com.apple.quicktime.creationdate"
    hdlr = box(b"hdlr", struct.pack(">I", 0) + b"\0\0\0\0" + b"mdta" + b"\0" * 12 + b"\0")
    keys = box(b"keys", struct.pack(">II", 0, 1) + struct.pack(">I4s", 8 + len(key), b"mdta") + key)
    data = box(b"data", struct.pack(">II", 1, 0) + value)
    item = struct.pack(">I", 8 + len(data)) + struct.pack(">I", 1) + data
    ilst = box(b"ilst", item)
    meta = box(b"meta", struct.pack(">I", 0) + hdlr + keys + ilst)
    ftyp = box(b"ftyp", b"qt  " + struct.pack(">I", 0) + b"qt  ")
    return ftyp + box(b"moov", mvhd(secs(d)) + meta) + box(b"mdat", b"\0" * 16)
open(f"{out}/synth-apple.mov", "wb").write(apple_mov(b"2023-01-02T07:34:56-0500"))
# hostile: atom claiming 4 GiB, deeply nested moov
open(f"{out}/hostile-bigatom.mp4", "wb").write(
    box(b"ftyp", b"isom" + b"\0" * 4 + b"isom") + struct.pack(">I4s", 0xFFFFFFF0, b"moov") + b"\0" * 32)
nest = box(b"free", b"")
for _ in range(5000):
    nest = box(b"moov", nest)
open(f"{out}/hostile-nested.mp4", "wb").write(box(b"ftyp", b"isom" + b"\0" * 4 + b"isom") + nest)
# hostile: zero-size atom loop
open(f"{out}/hostile-zero-atoms.mp4", "wb").write(
    box(b"ftyp", b"isom" + b"\0" * 4 + b"isom") + struct.pack(">I4s", 8, b"moov") * 1000)
# hostile JPEG: APP1 Exif with IFD entry count 65535 and self-referencing next-IFD offset
tiff = b"II*\0" + struct.pack("<I", 8) + struct.pack("<H", 65535) + b"\0" * 24 + struct.pack("<I", 8)
app1 = b"Exif\0\0" + tiff
open(f"{out}/hostile-ifd.jpg", "wb").write(b"\xff\xd8\xff\xe1" + struct.pack(">H", len(app1) + 2) + app1 + b"\xff\xd9")
# hostile JPEG: IFD loop (next IFD offset points to itself, 1 entry)
tiff = b"II*\0" + struct.pack("<I", 8) + struct.pack("<H", 0) + struct.pack("<I", 8)
app1 = b"Exif\0\0" + tiff
open(f"{out}/hostile-ifdloop.jpg", "wb").write(b"\xff\xd8\xff\xe1" + struct.pack(">H", len(app1) + 2) + app1 + b"\xff\xd9")
# truncated and empty files
open(f"{out}/empty.jpg", "wb").write(b"")
open(f"{out}/truncated.jpg", "wb").write(b"\xff\xd8\xff\xe1\x10\x00Exif\0\0II*")
