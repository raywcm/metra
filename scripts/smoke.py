#!/usr/bin/env python3
"""Real PTY smoke test: keys, search, resize, pause and terminal restoration.
Uses the Python standard library; never sends a process termination key.
"""
import argparse
import errno
import json
import tempfile
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import termios
import time

parser = argparse.ArgumentParser()
parser.add_argument('binary', nargs='?', default='bin/metra')
parser.add_argument('--capture', type=Path)
parser.add_argument('--warmup',type=float,default=2.0)
parser.add_argument('--language',choices=['en','zh-CN'],default='en')
parser.add_argument('--theme',choices=['auto','dark','light'],default='auto')
parser.add_argument('--terminal-background',choices=['dark','light'],default='dark')
parser.add_argument('--apple-accelerators',action='store_true')
parser.add_argument('--ascend',action='store_true')
parser.add_argument('--nvidia-engines',action='store_true')
parser.add_argument('--nvidia-bandwidth',action='store_true')
parser.add_argument('--responsive',action='store_true')
parser.add_argument('--exit-method',choices=['q','ctrl-c','sigterm'],default='q')
args = parser.parse_args()
binary = str(Path(args.binary).resolve())
build_version = subprocess.check_output([binary, '--version'], text=True).strip().split(' ', 1)[1]
config_dir=tempfile.TemporaryDirectory(prefix="metra-smoke-")
config_path=Path(config_dir.name)/"settings.json"
pid, fd = pty.fork()
if pid == 0:
    os.environ.pop('NO_COLOR',None)
    os.environ.update(TERM='xterm-256color', COLORTERM='truecolor')
    os.environ.update(LANG='en_US.UTF-8',LC_ALL='en_US.UTF-8')
    os.execv(binary, [binary, '--config', str(config_path),'--language',args.language,'--theme',args.theme])
all_output = bytearray()
query_tail=b''
zh=args.language=='zh-CN'
def label(en,cn):return (cn if zh else en).encode()

def resize(w, h):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', h, w, 0, 0))
    os.kill(pid, signal.SIGWINCH)

def drain(seconds=1.0):
    global query_tail
    end = time.monotonic() + seconds
    data = bytearray()
    while time.monotonic() < end:
        if not select.select([fd], [], [], min(.1, max(0, end-time.monotonic())))[0]:
            continue
        try:
            b = os.read(fd, 65536)
        except OSError as exc:
            if exc.errno == errno.EIO:
                break
            raise
        if not b:
            break
        query=query_tail+b
        if b'\x1b]11;?' in query:
            rgb=b'ffff/ffff/ffff' if args.terminal_background=='light' else b'1010/1717/2424'
            os.write(fd,b'\x1b]11;rgb:'+rgb+b'\x1b\\')
        query_tail=query[-6:]
        data.extend(b)
    all_output.extend(data)
    return bytes(data)

def send(keys, wait=.7):
    os.write(fd, keys)
    return drain(wait)

try:
    resize(120, 36)
    startup = drain(3.0)
    assert label('Performance','性能总览') in startup and b'CPU' in startup, 'missing overview'
    assert label('LIVE','实时') not in startup, 'redundant live status still visible'
    assert not any(0x2800 <= ord(c) <= 0x28ff for c in startup.decode('utf-8','replace')), 'obsolete Braille chart output'
    assert b'100%' not in startup, 'full-scale label should be hidden'
    assert label('CPU capacity','可用 CPU 配额') not in startup and label('Shared host','共享主机') not in startup, 'resource quota summary should be hidden'
    if args.ascend:
        assert b'GPU0' not in startup, 'virtual display exposed as a GPU'
        assert b'NPU0' in startup, 'Ascend NPU missing'
    drain(args.warmup)
    if args.capture:
        args.capture.mkdir(parents=True, exist_ok=True)
        (args.capture / 'performance.ansi').write_bytes(all_output)
    process = send(b'\x1b[C', 2.0)
    assert label('Processes','进程') in process and b'RSS' in process, 'missing process table'
    if args.capture:
        drain(2.0)
        (args.capture/'processes.ansi').write_bytes(all_output)
    send(b'/metra\r')
    send(b'\x1b', .2)
    send(b'ts')
    cpu = send(b'\x1b[C')
    assert b'CPU' in cpu, 'CPU page missing'
    if args.ascend or args.nvidia_engines:
        assert b'MHz' in cpu, 'live CPU frequency missing'
    send(b'c')
    send(b'c')
    if args.capture: (args.capture/'cpu.ansi').write_bytes(all_output)
    resize(45, 14)
    drain(.5)
    if args.responsive:
        for columns,rows in [(80,24),(40,12),(24,8)]:
            resize(columns,rows)
            small=drain(.7)
            assert label('enlarge terminal','放大终端') not in small, 'small viewport lost live numbers'
            if args.capture: (args.capture/f'cpu-{columns}x{rows}.ansi').write_bytes(all_output)
    resize(120, 36)
    drain(.5)
    memory=send(b'\x1b[C')
    assert label('Memory composition','内存组成') in memory, 'memory composition bar missing'
    if args.capture: (args.capture/'memory.ansi').write_bytes(all_output)
    disk=send(b'\x1b[C')
    assert label('Combined read/write','总读写') in disk, 'combined disk utilization missing'
    assert label('Read ','读取 ') in disk and label('Write ','写入 ') in disk, 'disk throughput missing'
    if args.capture: (args.capture/'disk.ansi').write_bytes(all_output)
    gpu_found,npu_found,ascend_found,nvidia_found=False,False,False,False
    nvidia_pages=0
    settings_page_data=b''
    if args.capture or args.apple_accelerators or args.ascend or args.nvidia_engines or args.nvidia_bandwidth:
        for _ in range(24):
            page=send(b'\x1b[C',.35)
            if label('Renderer','渲染器') in page:
                gpu_found=True
                if args.apple_accelerators:
                    assert b'N/A' not in page, 'unknown GPU fields should be hidden'
                    assert label('GPU memory','显存') in page or label('Shared GPU memory','共享显存') in page, 'GPU memory bar missing'
                if args.capture: (args.capture/'gpu.ansi').write_bytes(all_output)
            if b'Apple Neural Engine' in page:
                npu_found=True
                if args.capture: (args.capture/'npu.ansi').write_bytes(all_output)
            if b'Ascend' in page and b'HBM' in page:
                ascend_found=True
                assert b'AI Vector' in page and b'AI CPU' in page, 'Ascend engines missing'
                if args.capture: (args.capture/'npu.ansi').write_bytes(all_output)
            if b'NVIDIA' in page and b'MHz' in page:
                first_nvidia=not nvidia_found
                nvidia_found=True
                if args.nvidia_engines:
                    assert label('Encoder','编码') in page and label('Decoder','解码') in page, 'NVIDIA media engines missing'
                    assert label('Memory controller','显存控制器') in page, 'NVIDIA memory controller missing'
                if args.nvidia_bandwidth and first_nvidia:
                    assert label('PCIe RX','PCIe 接收') in page and label('TX','发送') in page, 'PCIe traffic missing'
                    if b'H200' in page or b'H100' in page:
                        assert label('HBM bandwidth','HBM 带宽') in page, 'HBM bandwidth missing'
                if args.responsive and first_nvidia:
                    for columns,rows in [(80,24),(40,12),(24,8)]:
                        resize(columns,rows)
                        drain(.7)
                        if args.capture: (args.capture/f'gpu-{columns}x{rows}.ansi').write_bytes(all_output)
                    resize(120,36)
                    drain(.7)
                if args.capture and first_nvidia: (args.capture/'gpu-first.ansi').write_bytes(all_output)
                if args.capture: (args.capture/f'gpu-{nvidia_pages}.ansi').write_bytes(all_output)
                nvidia_pages+=1
                if args.capture: (args.capture/'gpu.ansi').write_bytes(all_output)
            if label('Sampling interval','采样间隔') in page:
                settings_page_data=page
                break
        if args.apple_accelerators: assert gpu_found and npu_found, 'Apple GPU/NPU page missing'
        if args.ascend: assert ascend_found, 'Ascend NPU telemetry page missing'
        if args.nvidia_engines or args.nvidia_bandwidth: assert nvidia_found, 'NVIDIA engines or frequency missing'
    settings = settings_page_data+send(b',')
    assert label('Sampling interval','采样间隔') in settings and label('Color theme','配色主题') in settings, 'settings missing'
    assert label('Version','版本号') in settings, 'build version missing in settings'
    assert build_version.encode() in settings, 'settings version differs from executable version'
    assert label('Chart characters','图表字符') not in settings, 'obsolete chart mode setting remains'
    if args.theme=='auto':
        appearance='light' if args.terminal_background=='light' else 'dark'
        expected=f'Follow terminal ({appearance})'
        translated='跟随终端（浅色）' if appearance=='light' else '跟随终端（深色）'
        assert label(expected,translated) in settings, 'automatic terminal theme was not detected'
    # Arrows adjust the settings draft; Enter saves, Esc cancels.
    changed=send(b'\x1b[C')
    assert ('界面语言'.encode() if not zh else b'Language') in changed, 'live language switch failed'
    send(b'\x1b[D')
    send(b'\x1b[B')  # theme row
    if args.theme=='auto':
        send(b'\x1b[C\x1b[C')
        send(b'\x1b[D\x1b[D')
    if args.capture: (args.capture/'settings.ansi').write_bytes(all_output)
    saved=send(b'\r')
    save_deadline=time.monotonic()+5
    while label('Settings saved','设置已保存') not in saved and time.monotonic()<save_deadline:
        saved+=drain(.2)
    assert label('Settings saved','设置已保存') in saved, 'Enter did not save settings'
    persisted=json.loads(config_path.read_text())
    assert persisted['language']==args.language and persisted['theme']==args.theme, 'saved settings differ from draft'
    assert 'chart_style' not in persisted, 'obsolete chart mode setting saved'
    send(b'\r')  # re-enter editor
    send(b'\x1b[C')
    send(b'\x1b')  # discard preview
    send(b'\t')    # back to overview
    paused=send(b' ')
    assert label('PAUSED','已暂停') in paused, 'space did not pause sampling'
    resumed=send(b' ')
    assert resumed and label('PAUSED','已暂停') not in resumed, 'space did not clear paused status'
    send(b'?')
    send(b'\x1b')
    if args.exit_method=='sigterm':
        os.kill(pid,signal.SIGTERM)
        drain(.7)
    else:
        send(b'\x03' if args.exit_method=='ctrl-c' else b'q')
    deadline = time.monotonic() + 4
    status = None
    while time.monotonic() < deadline:
        done, status, usage = os.wait4(pid, os.WNOHANG)
        if done:
            break
        drain(.1)
    else:
        raise AssertionError('metra did not quit')
    assert os.waitstatus_to_exitcode(status) == 0, f'exit status: {status}'
    assert b'\x1b[?1049l' in all_output, 'alternate screen not restored'
    print('PTY smoke passed: navigation, process search/tree/sort, cores, resize, settings, help, pause, ' + args.exit_method + ' exit')
finally:
    try:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
    except (ProcessLookupError, ChildProcessError):
        pass
    os.close(fd)
    config_dir.cleanup()
