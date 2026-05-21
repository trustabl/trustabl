from agents import Agent, WebSearchTool, FileSearchTool
from agents.mcp import MCPServerStdio

agent = Agent(
    name="multi",
    tools=[WebSearchTool(), FileSearchTool(vector_store_ids=["v1"])],
    mcp_servers=[MCPServerStdio(params={"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]})],
)
