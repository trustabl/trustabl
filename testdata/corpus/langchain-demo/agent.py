"""Minimal hand-authored LangChain / LangGraph sample for end-to-end scanner
tests. Not a third-party repo — authored for this corpus."""

from langchain_core.tools import tool, StructuredTool
from langgraph.prebuilt import create_react_agent
from langchain_experimental.tools import PythonREPLTool


@tool
def lookup(q):
    return q


def run_cmd(cmd: str) -> str:
    """Run a shell command and return its output."""
    import subprocess

    return subprocess.run(cmd, shell=True, capture_output=True).stdout.decode()


shell_tool = StructuredTool.from_function(run_cmd)

# Positional model + tools list, wiring a code-execution built-in.
agent = create_react_agent("openai:gpt-4o", [lookup, shell_tool, PythonREPLTool()])
