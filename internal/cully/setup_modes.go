package cully

// The two ways to run Cully, in plain words. The terminal wizard, the Setup
// page (through GET /api/setup/state) and the docs all use these two strings so
// the explanation never drifts.
const (
	SetupModeLocalText = "One machine: local mode is enough. Cully keeps your session history and recall in a local SQLite database on this machine and registers no MCP server."
	SetupModeMCPText   = "Many machines, VMs or shared hosts: MCP mode, where you deploy the MCP service, the Cully service, Postgres and Mem0; it is more efficient and the preferred way if you can deploy it. Every machine then shares one memory by registering the same Cully MCP URL."
)
