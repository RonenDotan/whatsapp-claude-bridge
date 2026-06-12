#!/usr/bin/env python3
"""
Patch signal-service-java jar to fix NullPointerException on getServerGuid.

Issue #41: Kotlin null assertion in SignalServiceContent$Companion throws NPE
when Signal servers don't populate the serverGuid field, causing ALL incoming
Signal messages to fail silently.

Fix: NOP out the 7-byte null-check sequence (DUP + LDC_W + INVOKESTATIC) in
all affected locations. Processing continues with a null serverGuid instead of
crashing.

Usage:
    python _patch_signal_jar.py <path/to/signal-service-java-*.jar>

Exit codes:
    0  Success (patch applied, already patched, or not needed)
    1  Error
"""

import sys
import zipfile
import os
import shutil
import struct
import io

TARGET_CLASS = (
    'org/whispersystems/signalservice/api/messages/'
    'SignalServiceContent$Companion.class'
)


def parse_constant_pool(data):
    """Parse Java class constant pool. Returns list (1-indexed; index 0 is None)."""
    f = io.BytesIO(data)
    f.read(8)  # magic, minor_version, major_version
    cp_count = struct.unpack('>H', f.read(2))[0]
    cp = [None]
    i = 1
    while i < cp_count:
        tag = struct.unpack('>B', f.read(1))[0]
        if tag == 1:    # Utf8
            length = struct.unpack('>H', f.read(2))[0]
            cp.append(('Utf8', f.read(length).decode('utf-8', errors='replace')))
        elif tag == 7:   cp.append(('Class',     struct.unpack('>H',  f.read(2))[0]))
        elif tag == 8:   cp.append(('String',    struct.unpack('>H',  f.read(2))[0]))
        elif tag == 9:   cp.append(('Fieldref',  struct.unpack('>HH', f.read(4))))
        elif tag == 10:  cp.append(('Methodref', struct.unpack('>HH', f.read(4))))
        elif tag == 11:  cp.append(('IMethodref',struct.unpack('>HH', f.read(4))))
        elif tag == 12:  cp.append(('NameType',  struct.unpack('>HH', f.read(4))))
        elif tag == 3:   cp.append(('Integer',   f.read(4)))
        elif tag == 4:   cp.append(('Float',     f.read(4)))
        elif tag == 5:   cp.append(('Long',      f.read(8))); cp.append(None); i += 1
        elif tag == 6:   cp.append(('Double',    f.read(8))); cp.append(None); i += 1
        elif tag == 15:  cp.append(('MethodHandle', f.read(3)))
        elif tag == 16:  cp.append(('MethodType',   f.read(2)))
        elif tag == 17:  cp.append(('Dynamic',      f.read(4)))
        elif tag == 18:  cp.append(('InvokeDyn',    f.read(4)))
        elif tag == 19:  cp.append(('Module',       f.read(2)))
        elif tag == 20:  cp.append(('Package',      f.read(2)))
        else:            cp.append(('Unknown', tag)); break
        i += 1
    return cp


def find_null_check_offsets(class_data, cp):
    """
    Locate every occurrence of the 7-byte null-check pattern:
        DUP (0x59)
        LDC_W <cp_index_of_String "getServerGuid(...)">   (0x13 hi lo)
        INVOKESTATIC <cp_index_of_Intrinsics.checkNotNullExpressionValue>  (0xB8 hi lo)

    Returns (pattern_bytes, list_of_offsets).
    Returns (None, []) if the pattern cannot be built (bug fixed upstream).
    """
    # 1. Find String CP entry pointing to Utf8 "getServerGuid(...)"
    guid_str_idx = None
    for idx, entry in enumerate(cp):
        if entry and entry[0] == 'String':
            utf8_idx = entry[1]
            if (utf8_idx < len(cp) and cp[utf8_idx] and
                    cp[utf8_idx][0] == 'Utf8' and
                    cp[utf8_idx][1] == 'getServerGuid(...)'):
                guid_str_idx = idx
                break
    if guid_str_idx is None:
        return None, []  # string not present — likely fixed upstream

    # 2. Find Methodref for Intrinsics.checkNotNullExpressionValue
    check_idx = None
    for idx, entry in enumerate(cp):
        if entry and entry[0] == 'Methodref':
            class_ref, nat_ref = entry[1]
            if class_ref < len(cp) and nat_ref < len(cp):
                class_entry = cp[class_ref]
                nat_entry   = cp[nat_ref]
                if class_entry and nat_entry:
                    cn_idx  = class_entry[1] if class_entry[0] == 'Class' else None
                    mn_idx  = nat_entry[1][0] if nat_entry[0] == 'NameType' else None
                    if cn_idx and mn_idx and cn_idx < len(cp) and mn_idx < len(cp):
                        class_name  = cp[cn_idx][1]  if cp[cn_idx]  and cp[cn_idx][0]  == 'Utf8' else ''
                        method_name = cp[mn_idx][1]  if cp[mn_idx]  and cp[mn_idx][0]  == 'Utf8' else ''
                        if 'Intrinsics' in class_name and 'checkNotNullExpressionValue' in method_name:
                            check_idx = idx
                            break
    if check_idx is None:
        return None, []  # method not present — likely fixed upstream

    pattern = bytes([
        0x59,                                                    # DUP
        0x13, (guid_str_idx >> 8) & 0xFF, guid_str_idx & 0xFF,  # LDC_W
        0xB8, (check_idx >> 8) & 0xFF, check_idx & 0xFF,        # INVOKESTATIC
    ])

    offsets = []
    pos = 0
    while True:
        i = class_data.find(pattern, pos)
        if i == -1:
            break
        offsets.append(i)
        pos = i + 7
    return pattern, offsets


def patch_jar(jar_path):
    """
    Detect and apply the getServerGuid NPE patch to a signal-service-java jar.
    Returns (patched_count, message_string).
    """
    jar_name = os.path.basename(jar_path)

    with zipfile.ZipFile(jar_path, 'r') as zf:
        if TARGET_CLASS not in zf.namelist():
            return 0, f'[SKIP] {jar_name}: target class not found — wrong jar?'
        class_data = bytearray(zf.read(TARGET_CLASS))

    cp = parse_constant_pool(bytes(class_data))
    pattern, offsets = find_null_check_offsets(class_data, cp)

    if pattern is None:
        return 0, (
            f'[OK] {jar_name}: getServerGuid null-check not present — '
            'no patch needed (bug fixed upstream or structure changed)'
        )

    if not offsets:
        return 0, (
            f'[OK] {jar_name}: pattern located but 0 occurrences found — '
            'already patched or not applicable'
        )

    # Apply: replace each 7-byte pattern with NOPs
    nops = bytes(7)
    for offset in offsets:
        class_data[offset:offset + 7] = nops

    # Back up original (once only)
    backup = jar_path + '.bak'
    if not os.path.exists(backup):
        shutil.copy2(jar_path, backup)

    # Rebuild jar with patched class
    tmp_path = jar_path + '.patching'
    with zipfile.ZipFile(jar_path, 'r') as zin:
        with zipfile.ZipFile(tmp_path, 'w', compression=zipfile.ZIP_DEFLATED) as zout:
            for info in zin.infolist():
                content = bytes(class_data) if info.filename == TARGET_CLASS else zin.read(info.filename)
                zout.writestr(info, content)
    os.replace(tmp_path, jar_path)

    return len(offsets), (
        f'[PATCHED] {jar_name}: getServerGuid NPE fix applied at '
        f'{len(offsets)} location(s) — issue #41'
    )


if __name__ == '__main__':
    if len(sys.argv) < 2:
        print(f'Usage: python {os.path.basename(sys.argv[0])} <signal-service-java-*.jar>')
        sys.exit(1)

    jar_path = sys.argv[1]
    if not os.path.exists(jar_path):
        print(f'[ERROR] File not found: {jar_path}')
        sys.exit(1)

    count, msg = patch_jar(jar_path)
    print(msg)
    sys.exit(0)
