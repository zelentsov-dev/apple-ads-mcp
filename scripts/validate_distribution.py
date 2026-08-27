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
    upstream = json.loads((ROOT / "api-contract/upstream-baseline.json").read_text())
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
    require(0 < len(registry["description"]) <= 100, "registry description must contain 1 to 100 characters")
    require(registry["version"] == version, "registry version mismatch")
    require(registry["packages"][0]["identifier"].endswith(f":v{version}"), "registry package version mismatch")
    require(operations["productVersion"] == version, "operation matrix version mismatch")
    require(upstream["javaClientVersion"] == "v1.109.0", "unexpected Apple Java client baseline")
    require(f'var Version = "{version}"' in server, "server version mismatch")
    require(f"ARG VERSION={version}" in dockerfile, "Docker default version mismatch")
    require(f"`v{version}` is the current release" in readme, "README release status mismatch")
    require("release:" in goreleaser and 'name_template: "{{ .Tag }}"' in goreleaser, "GoReleaser release metadata missing")
    require("first public release" not in goreleaser, "GoReleaser still assumes a first public release")
    require("go test -race ./..." in release_workflow, "release race test missing")
    require("uses: ./.github/workflows/registry-publish.yml" in release_workflow, "release registry publication missing")
    require("PLUGIN_VERSION" in release_workflow, "release tag is not bound to plugin version")
    require("HOMEBREW_TAP_TOKEN" in release_workflow, "Homebrew tap publication token missing")
    trust_command = "brew trust --formula zelentsov-dev/tap/apple-ads-mcp"
    require(trust_command in release_workflow, "formula-scoped Homebrew trust gate missing")
    require("brew audit --strict --online" in release_workflow, "Homebrew audit gate missing")
    install_command = 'brew install --formula "$TAP_REPOSITORY/Formula/apple-ads-mcp.rb"'
    require(install_command in release_workflow, "Homebrew install must use the verified tap file")
    require(
        release_workflow.index(trust_command) < release_workflow.index("brew audit --strict --online") < release_workflow.index(install_command),
        "formula trust must precede Homebrew audit and install",
    )
    require("repos/zelentsov-dev/homebrew-tap/contents/Formula/apple-ads-mcp.rb" in release_workflow, "Homebrew tap target mismatch")
    require("workflow_call:" in registry_workflow, "registry workflow is not reusable")
    require(f'default: "{version}"' in registry_workflow, "registry workflow default version mismatch")
    require("ref: refs/tags/v${{ inputs.version }}" in registry_workflow, "registry workflow must check out the exact released tag")
    require('test "$(git rev-parse HEAD)" = "$TAG_COMMIT"' in registry_workflow, "registry workflow does not bind metadata to the tag commit")
    require("name: apple-ads-operator" in skill, "skill frontmatter missing")
    areas = [item["area"] for item in operations["operations"]]
    require(len(areas) == len(set(areas)), "operation areas must be unique")
    for reference in ["installation.md", "onboarding.md", "tool-routing.md", "campaign-workflows.md", "safety.md"]:
        require((ROOT / "skills/apple-ads-operator/references" / reference).is_file(), f"missing {reference}")
    require("brew install zelentsov-dev/tap/apple-ads-mcp" in readme, "canonical Homebrew install missing")
    require("codex mcp add apple-ads" in readme, "Codex MCP setup missing")
    require("claude mcp add --scope user apple-ads" in readme, "Claude user MCP setup missing")
    require("brew install --formula ./apple-ads-mcp.rb" not in readme, "unsupported local Homebrew formula install remains")
    require((ROOT / "docs/MIGRATION-v0.2.md").is_file(), "v0.2 migration notes missing")
    require((ROOT / "docs/MIGRATION-v0.3.md").is_file(), "v0.3 migration notes missing")
    require((ROOT / "docs/MIGRATION-v0.3.1.md").is_file(), "v0.3.1 migration notes missing")
    require("optimization-read" in areas and "optimization-apply" in areas, "optimization operation coverage missing")
    require("resource-lifecycle" in areas and "shared-budgets" in areas, "v0.3 lifecycle coverage missing")
    lifecycle = next(item for item in operations["operations"] if item["area"] == "resource-lifecycle")
    require(lifecycle["classification"] == "destructive-mutation", "delete classification mismatch")
    require("--allow-deletes" in readme and "APPLE_ADS_ALLOW_DELETES" in readme, "README delete gates missing")
    require("optimization policy init" in readme and "optimization_plan" in readme, "README optimizer setup missing")
    require("maxBid" in readme and "reconciliation" in readme.lower(), "README optimizer safety migration missing")
    require("scripts/audit_upstream.py" in (ROOT / ".github/workflows/upstream-audit.yml").read_text(), "upstream audit script is not scheduled")
    for source in ROOT.rglob("*.go"):
        if source.name.endswith("_test.go"):
            continue
        require("raw_request" not in source.read_text(), f"raw_request found in {source}")


if __name__ == "__main__":
    main()
