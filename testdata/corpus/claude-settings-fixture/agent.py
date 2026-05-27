from claude_agent_sdk import AgentDefinition

researcher = AgentDefinition(
    description="Reads files only.",
    prompt="You are a careful reader.",
    tools=["Read", "Glob", "Grep"],
    disallowedTools=["Bash", "Write", "Edit"],
    permissionMode="default",
    mcpServers=["github"],
)
