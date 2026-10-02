#!/usr/bin/env python3
"""使用标准库构建按架构分开的 VoCat 安装包，固定并校验官方内核。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parent
VERSION = 'v26.3.27'
ASSETS = {
    'amd64': ('Xray-linux-64.zip', '23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae'),
    'arm64': ('Xray-linux-arm64-v8a.zip', '4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c'),
    'windows-test': ('Xray-windows-64.zip', 'd004c39288ce9ada487c6f398c7c545f7d749e44bdfdd59dbc9f865afba4e1ad'),
}

def core_archive(arch, offline):
    name, expected = ASSETS[arch]
    path = ROOT / '.cache' / ('xray-' + VERSION) / name
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists():
        if offline:
            raise RuntimeError(f'缓存缺失：{path}')
        url = f'https://github.com/XTLS/Xray-core/releases/download/{VERSION}/{name}'
        temporary = path.with_suffix('.download')
        print(f'下载 {url}', flush=True)
        with urllib.request.urlopen(url, timeout=120) as response, temporary.open('wb') as output:
            while chunk := response.read(1024 * 1024):
                output.write(chunk)
        temporary.replace(path)
    actual = hashlib.sha256(path.read_bytes()).hexdigest()
    if actual != expected:
        raise RuntimeError(f'{path.name} SHA256 不匹配（可能下载未完成），拒绝打包')
    return path

def add_bytes(archive, name, data, executable=False):
    info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
    info.create_system = 3
    info.external_attr = (0o100755 if executable else 0o100644) << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    archive.writestr(info, data)

def source_files():
    names = ['go.mod', 'main.go', 'main_test.go', 'build.py', 'browser-test.cjs', 'vocat-plugin.json', 'README.md', 'LICENSE', 'SECURITY.md', 'THIRD_PARTY.md', '.gitignore']
    files = [ROOT / name for name in names]
    files += sorted((ROOT / 'internal').rglob('*.go'))
    files += sorted((ROOT / 'web').glob('*'))
    return [path for path in files if path.is_file()]

def build_source():
    manifest = json.loads((ROOT / 'vocat-plugin.json').read_text(encoding='utf-8'))
    output = ROOT / 'dist' / f'xray-manager-{manifest["version"]}-source.zip'
    output.parent.mkdir(exist_ok=True)
    with zipfile.ZipFile(output, 'w') as package:
        for path in source_files():
            add_bytes(package, path.relative_to(ROOT).as_posix(), path.read_bytes())
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    output.with_suffix('.zip.sha256').write_text(f'{digest}  {output.name}\n', encoding='utf-8')
    print(f'{output.name}  {output.stat().st_size:,} bytes', flush=True)

def build(arch, offline):
    core = core_archive(arch, offline)
    if arch == 'windows-test':
        target = ROOT / '.cache' / 'test-bin'
        target.mkdir(exist_ok=True)
        with zipfile.ZipFile(core) as archive:
            (target / 'xray.exe').write_bytes(archive.read('xray.exe'))
        print(target / 'xray.exe')
        return
    stage = ROOT / '.cache' / ('build-' + arch)
    stage.mkdir(exist_ok=True)
    env = dict(os.environ, GOOS='linux', GOARCH=arch, CGO_ENABLED='0')
    subprocess.run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(stage / 'xray-manager'), '.'], cwd=ROOT, env=env, check=True)
    manifest = json.loads((ROOT / 'vocat-plugin.json').read_text(encoding='utf-8'))
    manifest['backend']['commands'] = {f'linux/{arch}': 'bin/xray-manager'}
    output = ROOT / 'dist' / f'xray-manager-{manifest["version"]}-linux-{arch}.zip'
    output.parent.mkdir(exist_ok=True)
    with zipfile.ZipFile(output, 'w') as package, zipfile.ZipFile(core) as upstream:
        add_bytes(package, 'vocat-plugin.json', json.dumps(manifest, ensure_ascii=False, indent=2).encode())
        add_bytes(package, 'bin/xray-manager', (stage / 'xray-manager').read_bytes(), True)
        add_bytes(package, 'bin/xray', upstream.read('xray'), True)
        # Xray 的 LICENSE 等随官方包一同保留；单节点路由不使用 geoip/geosite 数据库。
        for name in upstream.namelist():
            if Path(name).name.lower().startswith(('license', 'notice')):
                add_bytes(package, 'licenses/Xray-' + Path(name).name, upstream.read(name))
        for name in ['README.md', 'LICENSE', 'SECURITY.md', 'THIRD_PARTY.md']:
            add_bytes(package, name, (ROOT / name).read_bytes())
        for path in sorted((ROOT / 'web').glob('*')):
            if path.is_file():
                add_bytes(package, 'web/' + path.name, path.read_bytes())
        for path in source_files():
            add_bytes(package, 'source/' + path.relative_to(ROOT).as_posix(), path.read_bytes())
    if output.stat().st_size > 64 * 1024 * 1024:
        raise RuntimeError('安装包超过 VoCat 的 64 MiB 限制')
    digest = hashlib.sha256(output.read_bytes()).hexdigest()
    output.with_suffix('.zip.sha256').write_text(f'{digest}  {output.name}\n', encoding='utf-8')
    print(f'{output.name}  {output.stat().st_size:,} bytes\nSHA256 {digest}', flush=True)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--arch', choices=['all', *ASSETS], default='all')
    parser.add_argument('--offline', action='store_true', help='仅使用已下载并通过校验的内核')
    args = parser.parse_args()
    if args.arch != 'windows-test':
        build_source()
    for arch in (['amd64', 'arm64'] if args.arch == 'all' else [args.arch]):
        build(arch, args.offline)
