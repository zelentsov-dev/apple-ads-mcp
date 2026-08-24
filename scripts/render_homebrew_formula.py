#!/usr/bin/env python3

import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit("usage: render_homebrew_formula.py VERSION CHECKSUMS OUTPUT")
    version, checksum_path, output_path = sys.argv[1:]
    checksums = {}
    for line in Path(checksum_path).read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        checksums[name.lstrip("*")] = digest
    replacements = {
        "{{VERSION}}": version,
        "{{DARWIN_ARM64_SHA256}}": checksum(checksums, version, "darwin", "arm64"),
        "{{DARWIN_AMD64_SHA256}}": checksum(checksums, version, "darwin", "amd64"),
        "{{LINUX_ARM64_SHA256}}": checksum(checksums, version, "linux", "arm64"),
        "{{LINUX_AMD64_SHA256}}": checksum(checksums, version, "linux", "amd64"),
    }
    formula = Path("packaging/homebrew/apple-ads-mcp.rb.tmpl").read_text()
    for source, target in replacements.items():
        formula = formula.replace(source, target)
    Path(output_path).write_text(formula)


def checksum(checksums: dict[str, str], version: str, platform: str, architecture: str) -> str:
    name = f"apple-ads-mcp_{version}_{platform}_{architecture}.tar.gz"
    if name not in checksums:
        raise SystemExit(f"checksum missing for {name}")
    return checksums[name]


if __name__ == "__main__":
    main()
