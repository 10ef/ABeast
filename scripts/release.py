#!/usr/bin/env python3
"""Validate versions and assemble only the intended public release files."""
import argparse
import hashlib
import json
import re
import shutil
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("darwin-universal", "windows-amd64", "windows-arm64")


def version():
    if (ROOT / "LICENSE").read_bytes() != (ROOT / "resources/LICENSE").read_bytes():
        raise ValueError("resources/LICENSE must match LICENSE")
    config = json.loads((ROOT / "mygo.json").read_text())
    value = config["version"]
    if config["name"] != "ABeast" or not re.fullmatch(r"\d+\.\d+\.\d+", value):
        raise ValueError("Expected ABeast and a stable x.y.z version in mygo.json")
    return value


def check_tag(tag):
    expected = "v" + version()
    if tag != expected:
        raise ValueError(f"Tag must match mygo.json: expected {expected}")
    print(f"Validated {tag}")


def package(target, build, output):
    value = version()
    stage = build / target
    output.mkdir(parents=True, exist_ok=True)
    stem = f"ABeast-{value}-{target}"
    if target == "darwin-universal":
        images = list(stage.glob("*.dmg"))
        if len(images) != 1 or images[0].stat().st_size == 0:
            raise ValueError("Expected exactly one nonempty DMG")
        shutil.copy2(images[0], output / f"{stem}.dmg")
    else:
        # mygo stages required DLLs/resources alongside the executable.
        # Exclude optional NSIS installers; distribute a portable ZIP only.
        files = sorted(p for p in stage.rglob("*") if p.is_file()
                       and not p.name.startswith("ABeast Setup "))
        exe = stage / "ABeast.exe"
        if exe not in files or exe.stat().st_size == 0:
            raise ValueError("Missing ABeast.exe")
        with zipfile.ZipFile(output / f"{stem}.zip", "w", zipfile.ZIP_DEFLATED) as archive:
            for path in files:
                if path.is_symlink():
                    raise ValueError(f"Unexpected symlink: {path}")
                archive.write(path, Path("ABeast") / path.relative_to(stage))
            archive.writestr("ABeast/README.txt",
                             "ABeast\n\nKeep all extracted files together and run ABeast.exe.\n"
                             "Documentation: https://github.com/10ef/ABeast\n"
                             "License: see LICENSE and THIRD_PARTY_NOTICES.txt.\n")
    print(f"Packaged {target} into {output}")


def checksums(output):
    value = version()
    expected = {f"ABeast-{value}-darwin-universal.dmg",
                f"ABeast-{value}-windows-amd64.zip",
                f"ABeast-{value}-windows-arm64.zip"}
    actual = {p.name for p in output.iterdir() if p.name != "SHA256SUMS.txt"}
    if actual != expected:
        raise ValueError(f"Unexpected release files: expected {sorted(expected)}, got {sorted(actual)}")
    lines = []
    for name in sorted(expected):
        path = output / name
        if not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"Empty or missing asset: {name}")
        with path.open("rb") as source:
            digest = hashlib.file_digest(source, "sha256").hexdigest()
        lines.append(f"{digest}  {name}\n")
    (output / "SHA256SUMS.txt").write_text("".join(lines), encoding="utf-8")
    print("Verified three release assets and wrote SHA256SUMS.txt")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("check-tag").add_argument("tag")
    pack = sub.add_parser("package")
    pack.add_argument("target", choices=TARGETS)
    pack.add_argument("--build", type=Path, default=ROOT / "build")
    pack.add_argument("--output", type=Path, default=ROOT / "dist")
    sums = sub.add_parser("checksums")
    sums.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if args.command == "check-tag":
        check_tag(args.tag)
    elif args.command == "package":
        package(args.target, args.build, args.output)
    else:
        checksums(args.output)


if __name__ == "__main__":
    main()
