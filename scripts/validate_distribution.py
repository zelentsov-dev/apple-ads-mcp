#!/usr/bin/env python3

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


def main() -> None:
    plugin = json.loads((ROOT / ".codex-plugin/plugin.json").read_text())
    mcp = json.loads((ROOT / ".mcp.json").read_text())
    registry = json.loads((ROOT / "server.json").read_text())
    skill = (ROOT / "skills/apple-ads-operator/SKILL.md").read_text()
    require(plugin["name"] == "apple-ads-mcp", "plugin name mismatch")
    require(plugin["mcpServers"] == "./.mcp.json", "plugin MCP path mismatch")
    require("apple-ads" in mcp["mcpServers"], "apple-ads MCP server missing")
    require(mcp["mcpServers"]["apple-ads"]["args"] == ["serve", "--stdio"], "stdio command mismatch")
    require(registry["name"] == "io.github.zelentsov-dev/apple-ads-mcp", "registry name mismatch")
    require("name: apple-ads-operator" in skill, "skill frontmatter missing")
    for reference in ["onboarding.md", "tool-routing.md", "campaign-workflows.md", "safety.md"]:
        require((ROOT / "skills/apple-ads-operator/references" / reference).is_file(), f"missing {reference}")
    for source in ROOT.rglob("*.go"):
        if source.name.endswith("_test.go"):
            continue
        require("raw_request" not in source.read_text(), f"raw_request found in {source}")


if __name__ == "__main__":
    main()
