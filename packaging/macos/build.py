#!/usr/bin/env python3
"""Build a local, self-contained arm64 app and drag-install DMG on macOS."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import tempfile


def run(*arguments, capture=False, env=None):
    result = subprocess.run(arguments, check=True, text=True, capture_output=capture, env=env)
    return result.stdout if capture else None


def dependencies(binary):
    output = run("otool", "-L", str(binary), capture=True)
    return [line.strip().split(" (compatibility", 1)[0] for line in output.splitlines()[1:] if line.strip()]


def rpaths(binary):
    output = run("otool", "-l", str(binary), capture=True)
    return re.findall(r"cmd LC_RPATH\s+cmdsize \d+\s+path (.*?) \(offset", output)


def system_library(path):
    return path.startswith(("/usr/lib/", "/System/Library/"))


def resolve_library(reference, source, executable):
    if reference.startswith("@loader_path/"):
        return (source.parent / reference.removeprefix("@loader_path/")).resolve(strict=True)
    if reference.startswith("@rpath/"):
        suffix = reference.removeprefix("@rpath/")
        for entry in rpaths(source):
            base = entry.replace("@loader_path", str(source.parent)).replace("@executable_path", str(executable.parent))
            candidate = Path(base) / suffix
            if candidate.exists():
                return candidate.resolve()
        raise RuntimeError(f"Unresolved runtime library {reference} in {source}")
    return Path(reference).resolve(strict=True)


def bundle_mpv(source, helpers, frameworks, notices):
    executable = source.resolve(strict=True)
    pending = [(executable, helpers / "mpv-bin")]
    destinations = {executable: helpers / "mpv-bin"}
    names = {}
    formulae = set()
    manifest = []
    while pending:
        original, destination = pending.pop()
        shutil.copy2(original, destination)
        destination.chmod(0o755)
        parts = original.parts
        if "Cellar" in parts:
            index = parts.index("Cellar")
            formulae.add(Path(*parts[:index + 3]))
        changes = []
        for reference in dependencies(original):
            if system_library(reference):
                continue
            library = resolve_library(reference, original, executable)
            if library == original:  # A dylib's own LC_ID_DYLIB entry.
                continue
            if library not in destinations:
                if library.name in names and names[library.name] != library:
                    raise RuntimeError(f"Conflicting library basename {library.name}")
                names[library.name] = library
                destinations[library] = frameworks / library.name
                pending.append((library, destinations[library]))
            relative = os.path.relpath(destinations[library], destination.parent)
            changes.extend(["-change", reference, "@loader_path/" + relative])
        for entry in rpaths(original):
            if not system_library(entry):
                changes.extend(["-delete_rpath", entry])
        if destination.parent == frameworks:
            changes.extend(["-id", "@rpath/" + destination.name])
        if changes:
            run("install_name_tool", *changes, str(destination))
        manifest.append({"file": str(destination.relative_to(helpers.parent)), "source": str(original)})
    for formula in sorted(formulae):
        target = notices / formula.parent.name / formula.name
        target.mkdir(parents=True, exist_ok=True)
        candidates = [item for item in formula.iterdir() if item.is_file() and
                      any(word in item.name.lower() for word in ("license", "copying", "copyright", "sbom"))]
        for item in candidates:
            shutil.copy2(item, target / item.name)
    (notices / "bundled-libraries.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return list(destinations.values())


def go_licenses(go, notices):
    output = run(go, "list", "-mod=readonly", "-m", "-json", "all", capture=True)
    decoder = json.JSONDecoder()
    while output.strip():
        module, offset = decoder.raw_decode(output.lstrip())
        output = output.lstrip()[offset:]
        if module.get("Main") or not module.get("Dir"):
            continue
        directory = Path(module["Dir"])
        target = notices / "Go" / module["Path"] / module.get("Version", "local")
        for item in directory.iterdir():
            if item.is_file() and any(word in item.name.lower() for word in ("license", "copying", "copyright", "notice")):
                target.mkdir(parents=True, exist_ok=True)
                shutil.copy2(item, target / item.name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", default=shutil.which("go"))
    parser.add_argument("--mpv", default=shutil.which("mpv"))
    parser.add_argument("--sdk", default="macosx26.5")
    parser.add_argument("--output", type=Path, default=Path("dist/macos"))
    args = parser.parse_args()
    if sys.platform != "darwin" or os.uname().machine != "arm64":
        parser.error("This build currently targets Apple Silicon on macOS")
    if not args.go or not args.mpv:
        parser.error("Go and mpv are required for building")
    repo = Path(__file__).resolve().parents[2]
    source = Path(__file__).resolve().parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    app = output / "Molpe.app"
    if app.exists():
        info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
        if info.get("CFBundleIdentifier") != "local.molpe.player":
            raise RuntimeError(f"Refusing to replace an unrelated app at {app}")
        shutil.rmtree(app)
    contents = app / "Contents"
    helpers = contents / "Helpers"
    resources = contents / "Resources"
    scripts = resources / "bin"
    frameworks = contents / "Frameworks"
    macos = contents / "MacOS"
    notices = resources / "Licenses"
    for directory in (helpers, resources, frameworks, macos, notices, scripts):
        directory.mkdir(parents=True, exist_ok=True)
    info = {
        "CFBundleIdentifier": "local.molpe.player",
        "CFBundleName": "Molpe",
        "CFBundleDisplayName": "木末 Molpe",
        "CFBundleExecutable": "Molpe",
        "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": "1.3.0",
        "CFBundleVersion": "13006",
        "CFBundleIconFile": "Molpe.icns",
        "LSMinimumSystemVersion": "26.0",
        "LSUIElement": True,
        "NSHighResolutionCapable": True,
        "NSHumanReadableCopyright": "Molpe contributors. See bundled licenses.",
    }
    (contents / "Info.plist").write_bytes(plistlib.dumps(info))
    sdk = run("xcrun", "--sdk", args.sdk, "--show-sdk-path", capture=True).strip()
    print("Building Molpe and the native launcher", flush=True)
    build_env = os.environ | {"GOOS": "darwin", "GOARCH": "arm64", "CGO_ENABLED": "0"}
    run(args.go, "build", "-mod=readonly", "-trimpath", "-o", str(helpers / "molpe"), str(repo), env=build_env)
    run("clang", "-isysroot", sdk, "-fobjc-arc", "-mmacosx-version-min=26.0", "-framework", "AppKit",
        str(source / "Launcher.m"), "-o", str(macos / "Molpe"))
    for filename, directory in (("Player.command", resources), ("mpv", scripts)):
        shutil.copy2(source / filename, directory / filename)
        (directory / filename).chmod(0o755)
    (resources / "Certificates").mkdir()
    certificates = Path(run("brew", "--prefix", capture=True).strip()) / "etc/ca-certificates/cert.pem"
    shutil.copy2(certificates, resources / "Certificates/cert.pem")
    shutil.copy2(repo / "LICENSE", notices / "Molpe-MIT.txt")
    go_licenses(args.go, notices)
    print("Bundling mpv and its runtime libraries", flush=True)
    binaries = bundle_mpv(Path(args.mpv), helpers, frameworks, notices)
    with tempfile.TemporaryDirectory(prefix="molpe-icon-") as temporary:
        iconset = Path(temporary) / "Molpe.iconset"
        run("swift", "-sdk", sdk, str(source / "Icon.swift"), str(iconset))
        run("iconutil", "-c", "icns", str(iconset), "-o", str(resources / "Molpe.icns"))
    for binary in [*binaries, helpers / "molpe", macos / "Molpe"]:
        run("codesign", "--force", "--sign", "-", "--timestamp=none", str(binary))
    run("codesign", "--force", "--sign", "-", "--timestamp=none", str(app))
    run("codesign", "--verify", "--deep", "--strict", "--verbose=2", str(app))
    (output / "INSTALL.txt").write_text(
        "木末 Molpe 1.3.0 · Mac 本机测试版\n\n"
        "适用于 Apple Silicon 和 macOS 26。\n"
        "将 Molpe.app 拖入 Applications，再双击打开。应用会启动终端播放器。\n"
        "首次使用请在终端中扫描二维码登录网易云。q 完全退出，Q 保留后台播放。\n"
        "按 a 打开音质选择，方向键移动，Enter 应用并自动保存，Esc 取消。\n"
        "选歌后进入左侧封面和下方歌词、右侧播放列表的播放页。b/Esc 返回列表，v 再次打开播放页。\n"
        "右侧用 j/k 或方向键选曲，Enter 播放，n 加入下一首播放，g/G 跳到首尾。\n"
        "支持 Kitty 图片协议和 Unicode 图片占位符时自动显示图片，否则显示彩色色块。\n"
        "要使用 Kitty/Ghostty，请在该终端中运行 /Applications/Molpe.app/Contents/Resources/Player.command。\n"
        "本包包含 mpv 及其动态库。应用数据位于 ~/Library/Application Support/Molpe，"
        "缓存和后端日志位于 ~/Library/Caches/Molpe。\n"
        "卸载前按 q 结束播放，再把 Molpe.app 移到废纸篓。需要清除登录态时可另行删除上述数据目录。\n"
        "本包采用本机临时签名，尚未经过 Apple Developer ID 签名和公证。\n"
        "系统若阻止打开，请在确认文件来源后使用“系统设置 → 隐私与安全性 → 仍要打开”。\n"
        "第三方组件许可证位于应用包内 Contents/Resources/Licenses。\n", encoding="utf-8")
    dmg = output / "Molpe-1.3.0-mac.6-arm64.dmg"
    with tempfile.TemporaryDirectory(prefix="molpe-dmg-", dir=output) as temporary:
        staging = Path(temporary)
        shutil.copytree(app, staging / app.name, symlinks=True)
        (staging / "Applications").symlink_to("/Applications", target_is_directory=True)
        shutil.copy2(output / "INSTALL.txt", staging / "安装说明.txt")
        run("hdiutil", "create", "-ov", "-volname", "Molpe", "-srcfolder", str(staging),
            "-format", "UDZO", str(dmg))
    digest = hashlib.sha256(dmg.read_bytes()).hexdigest()
    (output / (dmg.name + ".sha256")).write_text(f"{digest}  {dmg.name}\n")
    print(f"Built {dmg}", flush=True)


if __name__ == "__main__":
    main()
