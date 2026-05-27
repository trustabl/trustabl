"""Minimal ADK fixture: triggers ADK-001 (no docstring), ADK-101
(no description), and ADK-102 (BashTool with no before_tool_callback).
"""

from google.adk.agents import LlmAgent
from google.adk.tools import FunctionTool, BashTool


def get_weather(city):  # intentionally no docstring -> ADK-001
    return "sunny"


root = LlmAgent(
    name="root",
    model="gemini-2.5-flash",
    instruction="You are a helpful assistant.",
    # intentionally no description= -> ADK-101
    tools=[FunctionTool(get_weather), BashTool()],
    # intentionally no before_tool_callback= -> ADK-102
)
