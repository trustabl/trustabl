"""Minimal fixture for the Stage 2 typed captures: a tool with a static
outbound URL + retry decorator, and a tool with static write-path literals.
All hosts are example.* placeholders. Exercised by the ACaC golden tests and
the OpenShell policy exporter goldens."""

from agents import Agent, function_tool
from tenacity import retry
import requests
import shutil


@function_tool
@retry
def fetch_status() -> str:
    """Fetch the service status page."""
    resp = requests.get("https://status.example.com/api/v1", timeout=10)
    return resp.text


@function_tool
def save_report(content: str) -> str:
    """Persist a report to the workspace output directory."""
    with open("/workspace/out/report.txt", "w") as f:
        f.write(content)
    shutil.copy("/workspace/out/report.txt", "/workspace/archive/report.txt")
    return "saved"


agent = Agent(
    name="Status Reporter",
    instructions="Report service status and persist findings to the workspace.",
    model="gpt-4o",
    tools=[fetch_status, save_report],
)
