const vscode = require("vscode");
const { execFile } = require("child_process");

const AGENTS = {
  runClaude: ["claude", "Claude Code"],
  runCodex: ["codex", "Codex"],
  runCursor: ["cursor-agent", "Cursor Agent"],
};

// Read-only commands render in the Cully output channel.
const READ_ONLY = {
  status: ["status"],
  harness: ["harness"],
  impact: ["impact", "--explain"],
  handoff: ["handoff", "--print"],
  replay: ["replay", "--steps"],
  improve: ["improve"],
};

let output;
let statusItem;
let timer;

const config = () => vscode.workspace.getConfiguration("cully");
const workspaceDir = () => vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;

function cully(args, cwd) {
  return new Promise((resolve) => {
    execFile(config().get("path"), args, { cwd, timeout: 60000, env: { ...process.env, NO_COLOR: "1" } },
      (error, stdout, stderr) => resolve({ error, text: `${stdout}${stderr}`.trim() }));
  });
}

async function showOutput(args) {
  const cwd = workspaceDir();
  const scoped = { status: [cwd], harness: ["--cwd", cwd], replay: ["--cwd", cwd], handoff: ["--cwd", cwd] }[args[0]];
  const full = cwd && scoped ? [...args, ...scoped] : args;
  output.show(true);
  output.appendLine(`$ cully ${full.join(" ")}`);
  const { error, text } = await cully(full, cwd);
  output.appendLine(text || (error ? error.message : "(no output)"));
  output.appendLine("");
}

function openTerminal(args, name) {
  const terminal = vscode.window.createTerminal({ name, cwd: workspaceDir() });
  terminal.show();
  terminal.sendText(["cully", ...args].map(quote).join(" "));
}

const quote = (s) => (/^[\w./:=@-]+$/.test(s) ? s : `'${s.replace(/'/g, "'\\''")}'`);

async function refreshStatus() {
  if (!config().get("statusBar")) return statusItem.hide();
  const { error, text } = await cully(["status", ...(workspaceDir() ? [workspaceDir()] : [])], workspaceDir());
  if (error && error.code === "ENOENT") {
    statusItem.text = "$(warning) Cully not found";
    statusItem.tooltip = "Install Cully or set cully.path.";
  } else {
    const risk = /Risk\s+(\w+)/.exec(text)?.[1];
    const verified = /Verification\s+(.+)/.exec(text)?.[1]?.trim();
    statusItem.text = `$(pulse) Cully${risk ? ` ${risk}` : ""}`;
    statusItem.tooltip = new vscode.MarkdownString(
      ["**Cully**", risk && `Risk: ${risk}`, verified && `Verification: ${verified}`, "Click for the menu."]
        .filter(Boolean).join("\n\n"));
  }
  statusItem.show();
}

function schedule() {
  clearInterval(timer);
  timer = setInterval(refreshStatus, Math.max(10, config().get("refreshSeconds")) * 1000);
}

async function menu() {
  const items = [
    ...Object.entries(AGENTS).map(([id, [, label]]) => ({ label: `$(terminal) Run ${label}`, id: `cully.${id}` })),
    { label: "$(pulse) Session status", id: "cully.status" },
    { label: "$(history) Replay session", id: "cully.replay" },
    { label: "$(arrow-right) Hand off session", id: "cully.handoff" },
    { label: "$(tools) Rescue stuck session", id: "cully.rescue" },
    { label: "$(checklist) Audit harness", id: "cully.harness" },
    { label: "$(graph) Token impact", id: "cully.impact" },
    { label: "$(lightbulb) Review self-improvements", id: "cully.improve" },
    { label: "$(tag) Set session task", id: "cully.setTask" },
  ];
  const pick = await vscode.window.showQuickPick(items, { placeHolder: "Cully" });
  if (pick) vscode.commands.executeCommand(pick.id);
}

function activate(context) {
  output = vscode.window.createOutputChannel("Cully");
  statusItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 50);
  statusItem.command = "cully.menu";
  context.subscriptions.push(output, statusItem, { dispose: () => clearInterval(timer) });

  const register = (id, fn) => context.subscriptions.push(vscode.commands.registerCommand(`cully.${id}`, fn));
  for (const [id, [agent, label]] of Object.entries(AGENTS)) register(id, () => openTerminal(["run", agent], `Cully: ${label}`));
  for (const [id, args] of Object.entries(READ_ONLY)) register(id, () => showOutput(args).then(refreshStatus));
  // Rescue may call a model, so it runs in a terminal where the user sees it.
  register("rescue", () => openTerminal(["rescue"], "Cully: Rescue"));
  register("setTask", async () => {
    const name = await vscode.window.showInputBox({ prompt: "Session task name (empty clears it)" });
    if (name === undefined) return;
    await showOutput(name ? ["task", name] : ["task", "--clear"]);
  });
  register("menu", menu);

  context.subscriptions.push(vscode.workspace.onDidChangeConfiguration((e) => {
    if (e.affectsConfiguration("cully")) { schedule(); refreshStatus(); }
  }));
  schedule();
  refreshStatus();
}

function deactivate() {}

module.exports = { activate, deactivate };
