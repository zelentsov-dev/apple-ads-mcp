#!/usr/bin/env python3

import argparse
import json
import os
import re
import sys
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BASELINE_PATH = ROOT / "api-contract/upstream-baseline.json"
ALLOWED_ROOTS = {
    "acls",
    "ad-accounts",
    "adgroups",
    "ads",
    "advertiser-resources",
    "apps",
    "campaigns",
    "change-history",
    "creatives",
    "eligibilities",
    "insights",
    "keywords",
    "me",
    "metadata",
    "negative-keywords",
    "orgs",
    "product-pages",
    "recommendations",
    "rejection-reasons",
    "reports",
    "search",
    "shared-budgets",
    "suggestions",
}
METHOD_ORDER = {"GET": 0, "POST": 1, "PUT": 2, "DELETE": 3}


def endpoint_key(endpoint: str) -> tuple[int, str]:
    method, path = endpoint.split(" ", 1)
    return METHOD_ORDER.get(method, 99), path


def load_baseline() -> dict:
    baseline = json.loads(BASELINE_PATH.read_text())
    version = baseline.get("javaClientVersion", "")
    endpoints = baseline.get("appStoreEndpoints", [])
    if re.fullmatch(r"v\d+\.\d+\.\d+", version) is None:
        raise SystemExit("upstream baseline has an invalid Java client version")
    if not endpoints or endpoints != sorted(set(endpoints), key=endpoint_key):
        raise SystemExit("upstream App Store endpoint inventory must be non-empty, unique, and sorted")
    return baseline


def request_text(url: str) -> str:
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "apple-ads-mcp-upstream-audit",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=30) as response:
        return response.read(8 * 1024 * 1024).decode("utf-8")


def app_store_endpoints(source: str) -> list[str]:
    endpoints = set()
    for method, path in re.findall(r'@(GET|POST|PUT|DELETE)\("([^"]+)"\)', source):
        root = path.split("/", 1)[0]
        if root not in ALLOWED_ROOTS or "business-brands" in path:
            continue
        endpoints.add(f"{method} /{path}")
    return sorted(endpoints, key=endpoint_key)


def write_summary(lines: list[str]) -> None:
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as output:
            output.write("\n".join(lines) + "\n")
    print("\n".join(lines))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--local", action="store_true", help="validate the pinned baseline without network access")
    args = parser.parse_args()
    baseline = load_baseline()
    if args.local:
        print(f"upstream baseline is valid: {baseline['javaClientVersion']} ({len(baseline['appStoreEndpoints'])} endpoints)")
        return

    latest = os.environ.get("APPLE_ADS_UPSTREAM_VERSION", "")
    if not latest:
        release = json.loads(request_text("https://api.github.com/repos/apple/apple-ads-platform-api-java/releases/latest"))
        latest = release.get("tag_name", "")
    if re.fullmatch(r"v\d+\.\d+\.\d+", latest) is None:
        raise SystemExit("latest Apple Java client release has an invalid tag")
    interface = baseline["apiInterface"]
    source_url = f"https://raw.githubusercontent.com/apple/apple-ads-platform-api-java/{latest}/{interface}"
    current = app_store_endpoints(request_text(source_url))
    expected = baseline["appStoreEndpoints"]
    added = sorted(set(current) - set(expected))
    removed = sorted(set(expected) - set(current))
    version_drift = latest != baseline["javaClientVersion"]

    status = "PASS" if not version_drift and not added and not removed else "DRIFT"
    lines = [
        "## Apple Ads upstream audit",
        "",
        f"- Status: **{status}**",
        f"- Pinned Java client: `{baseline['javaClientVersion']}`",
        f"- Latest Java client: `{latest}`",
        f"- Pinned App Store endpoints: `{len(expected)}`",
        f"- Latest App Store endpoints: `{len(current)}`",
    ]
    if added:
        lines.extend(["", "### Added endpoints", *[f"- `{item}`" for item in added]])
    if removed:
        lines.extend(["", "### Removed endpoints", *[f"- `{item}`" for item in removed]])
    if version_drift and not added and not removed:
        lines.extend(["", "The official client version changed without an App Store endpoint inventory change. Review model and enum changes before updating the baseline."])
    write_summary(lines)
    if status != "PASS":
        raise SystemExit("Apple Ads upstream drift detected; review the Actions summary")


if __name__ == "__main__":
    main()
