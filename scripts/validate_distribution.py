#!/usr/bin/env python3

import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


def main() -> None:
    plugin = json.loads((ROOT / ".codex-plugin/plugin.json").read_text())
    mcp = json.loads((ROOT / ".mcp.json").read_text())
    registry = json.loads((ROOT / "server.json").read_text())
    operations = json.loads((ROOT / "api-contract/operations.json").read_text())
    skill = (ROOT / "skills/apple-ads-operator/SKILL.md").read_text()
    readme = (ROOT / "README.md").read_text()
    server = (ROOT / "internal/mcpserver/server.go").read_text()
    dockerfile = (ROOT / "Dockerfile").read_text()
    goreleaser = (ROOT / ".goreleaser.yml").read_text()
    release_workflow = (ROOT / ".github/workflows/release.yml").read_text()
    registry_workflow = (ROOT / ".github/workflows/registry-publish.yml").read_text()
    version = plugin["version"]
    require(plugin["name"] == "apple-ads-mcp", "plugin name mismatch")
    require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version) is not None, "plugin version is not stable semver")
    require(plugin["mcpServers"] == "./.mcp.json", "plugin MCP path mismatch")
    require("apple-ads" in mcp["mcpServers"], "apple-ads MCP server missing")
    require(mcp["mcpServers"]["apple-ads"]["args"] == ["serve", "--stdio"], "stdio command mismatch")
    require(registry["name"] == "io.github.zelentsov-dev/apple-ads-mcp", "registry name mismatch")
    require(registry["version"] == version, "registry version mismatch")
    require(registry["packages"][0]["identifier"].endswith(f":v{version}"), "registry package version mismatch")
    require(f'var Version = "{version}"' in server, "server version mismatch")
    require(f"ARG VERSION={version}" in dockerfile, "Docker default version mismatch")
    require(f"`v{version}` is the first public release" in readme, "README release status mismatch")
    require("release:" in goreleaser and 'name_template: "{{ .Tag }}"' in goreleaser, "GoReleaser release metadata missing")
    require("go test -race ./..." in release_workflow, "release race test missing")
    require("uses: ./.github/workflows/registry-publish.yml" in release_workflow, "release registry publication missing")
    require("workflow_call:" in registry_workflow, "registry workflow is not reusable")
    require("name: apple-ads-operator" in skill, "skill frontmatter missing")
    areas = [item["area"] for item in operations["operations"]]
    require(len(areas) == len(set(areas)), "operation areas must be unique")
    for reference in ["onboarding.md", "tool-routing.md", "campaign-workflows.md", "safety.md"]:
        require((ROOT / "skills/apple-ads-operator/references" / reference).is_file(), f"missing {reference}")
    for source in ROOT.rglob("*.go"):
        if source.name.endswith("_test.go"):
            continue
        require("raw_request" not in source.read_text(), f"raw_request found in {source}")


if __name__ == "__main__":
    main()
