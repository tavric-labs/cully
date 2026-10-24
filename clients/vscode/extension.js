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
let dashboard;

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
  terminal.sendText([config().get("path") || "cully", ...args].map(quote).join(" "));
}

const quote = (s) => (/^[\w./:=@-]+$/.test(s) ? s : `'${s.replace(/'/g, "'\\''")}'`);

async function refreshStatus() {
  dashboard?.refresh();
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

async function readDashboard() {
  const cwd = workspaceDir();
  const dir = cwd || process.cwd();
  const [status, harness, impact, suggestions] = await Promise.all([
    cully(["status", ...(cwd ? [cwd] : [])], cwd),
    cully(["harness", "--cwd", dir], cwd),
    cully(["impact", "--explain"], cwd),
    cully(["suggestions"], cwd),
  ]);
  return {
    status: status.text || (status.error ? status.error.message : "No status data"),
    harness: harness.text || (harness.error ? harness.error.message : "No harness data"),
    impact: impact.text || (impact.error ? impact.error.message : "No activity data"),
    suggestions: suggestions.text || "No current advisor suggestions.",
    updated: new Date().toLocaleTimeString(),
    directory: cwd || "No workspace open",
  };
}

function dashboardHTML(webview) {
  const nonce = [...Array(24)].map(() => Math.random().toString(36).slice(2)).join("");
  return `<!doctype html>
<html><head><meta charset="UTF-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
body{padding:0 12px 18px;color:var(--vscode-foreground);font:var(--vscode-font-size) var(--vscode-font-family);line-height:1.45}
.top{display:flex;align-items:center;justify-content:space-between;position:sticky;top:0;background:var(--vscode-sideBar-background);padding:8px 0;z-index:1}
h1{font-size:15px;margin:0;font-weight:600}.muted,.foot{color:var(--vscode-descriptionForeground);font-size:11px}
button{font:inherit;color:var(--vscode-button-foreground);background:var(--vscode-button-background);border:0;border-radius:2px;padding:4px 8px;cursor:pointer}
button:hover{background:var(--vscode-button-hoverBackground)}button.secondary{color:var(--vscode-foreground);background:var(--vscode-button-secondaryBackground)}
section{border:1px solid var(--vscode-panel-border);border-radius:5px;margin:9px 0;padding:9px;background:var(--vscode-sideBar-background)}
h2{font-size:12px;text-transform:uppercase;letter-spacing:.04em;margin:0 0 7px;color:var(--vscode-descriptionForeground)}
.hero{display:flex;align-items:center;gap:8px;font-size:14px;font-weight:600}.dot{color:var(--vscode-testing-iconPassed)}.risk{color:var(--vscode-editorWarning-foreground)}
.row{display:flex;justify-content:space-between;gap:10px;padding:3px 0;border-bottom:1px solid var(--vscode-widget-border);font-size:12px}.row:last-child{border:0}.value{text-align:right;overflow-wrap:anywhere;color:var(--vscode-descriptionForeground)}
.chips{display:flex;gap:5px;flex-wrap:wrap}.chip{font-size:11px;padding:2px 6px;border-radius:9px;background:var(--vscode-badge-background);color:var(--vscode-badge-foreground)}
.advice{font-size:12px;margin:7px 0}.advice button{display:block;margin-top:6px}
.empty{color:var(--vscode-descriptionForeground);font-style:italic;font-size:12px}
pre{white-space:pre-wrap;overflow-wrap:anywhere;font:11px var(--vscode-editor-font-family);color:var(--vscode-descriptionForeground);margin:0}
</style></head>
<body><div class="top"><div><h1>Cully</h1><div class="muted" id="workspace"></div></div><button id="refresh" class="secondary" title="Refresh dashboard">↻</button></div>
<div id="content"><p class="empty">Loading session…</p></div><div id="updated" class="foot"></div>
<script nonce="${nonce}">
const vscode=acquireVsCodeApi();document.getElementById('refresh').onclick=()=>vscode.postMessage({type:'refresh'});
window.addEventListener('message',e=>render(e.data));
function card(title){const s=document.createElement('section'),h=document.createElement('h2');h.textContent=title;s.append(h);return s}
function row(parent,label,value){const d=document.createElement('div');d.className='row';const a=document.createElement('span'),b=document.createElement('span');a.textContent=label;b.className='value';b.textContent=value;d.append(a,b);parent.append(d)}
function section(text){const m=/^\\s*(.*?)\\s{2,}(.*)$/.exec(text);return m?[m[1].trim(),m[2].trim()]:null}
function render(data){
 document.getElementById('workspace').textContent=data.directory;document.getElementById('updated').textContent='Updated '+data.updated;
 const root=document.getElementById('content');root.replaceChildren();
 const lines=data.status.split('\\n'),health=card('Session'),risk=lines.find(x=>/^\\s*Risk\\s+/.test(x)),agent=lines.find(x=>/^\\s*Agent\\s+/.test(x));
 const hero=document.createElement('div');hero.className='hero';const dot=document.createElement('span');dot.className='dot';dot.textContent='●';const title=document.createElement('span');title.textContent=agent?agent.replace(/^\\s*Agent\\s+/, ''):'Session status';hero.append(dot,title);
 if(risk){const badge=document.createElement('span');badge.className='risk';badge.textContent=risk.replace(/^\\s*Risk\\s+/, '');hero.append(badge)}health.append(hero);
 for(const x of lines){const r=section(x);if(r&&['Branch','Session','Context','Tests','Verification','Loops','Uncommitted','Memory','Risk'].includes(r[0]))row(health,r[0],r[1])}root.append(health);
 const setup=card('Harness');const setupLine=data.harness.split('\\n').find(x=>/configured\\./.test(x));if(setupLine){const p=document.createElement('div');p.className='muted';p.textContent=setupLine.trim();setup.append(p)}
 const required=new Set(['Instructions','Guardrails','Memory','Skills','Hooks and scripts','Subagents','Plugins']);
 const components=data.harness.split('\\n').map(x=>x.match(/^\\s*([✓·])\\s+([^:]+):?\\s*(.*)$/)).filter(c=>c&&required.has(c[2].trim()));
 if(components.length){const chips=document.createElement('div');chips.className='chips';for(const c of components){const chip=document.createElement('span');chip.className='chip';chip.textContent=(c[1]==='✓'?'✓ ':'! ')+c[2].trim();chip.title=c[3].trim();chips.append(chip)}setup.append(chips)}
 const measureAt=data.harness.split('\\n').findIndex(x=>/^MEASURED/.test(x));if(measureAt>=0){const p=document.createElement('p');p.className='muted';p.textContent=data.harness.split('\\n').slice(measureAt,measureAt+3).join(' ');setup.append(p)}root.append(setup);
 const activity=card('Cully activity');for(const x of data.impact.split('\\n')){const r=section(x);if(r&&['Memory used','Analyses avoided','Loops caught','Oversized output flagged','Saved (estimated)','Cost (measured)','Net'].includes(r[0]))row(activity,r[0],r[1])}root.append(activity);
 const advisor=card('Advisor');let found=false;
 function adviceItem(text){found=true;const item=document.createElement('div');item.className='advice';const body=document.createElement('div');body.textContent=text;const button=document.createElement('button');button.textContent='Add advice to Agent Chat';button.onclick=()=>vscode.postMessage({type:'prompt',text:'Cully advisor suggestion: '+text+'. Review whether this fits the current task, then suggest or make the appropriate change.'});item.append(body,button);advisor.append(item)}
 for(const line of data.status.split('\\n')){const text=line.trim().replace(/^[○⚠]\\s*/, '');if(/^(automatic advisor|graphify graph|advisor:|warning:)/i.test(text))adviceItem(text)}
 for(const line of data.suggestions.split('\\n')){const m=line.match(/^\\[(\\d+)\\]\\s+(?:([^ ]+)\\s+)?(.*)$/);if(m)adviceItem(m[3])}
 if(!found){const p=document.createElement('p');p.className='empty';p.textContent='No current suggestions. Local session warnings remain available in Session status.';advisor.append(p)}
 const details=card('Details');const pre=document.createElement('pre');pre.textContent=data.status.split('Agent setup')[1]?'Agent setup'+data.status.split('Agent setup')[1]:data.status;details.append(pre);root.append(advisor,details);
}
</script></body></html>`;
}

class CullyDashboard {
  constructor(view) {
    this.view = view;
    this.refreshing = false;
    view.webview.options = { enableScripts: true };
    view.webview.html = dashboardHTML(view.webview);
    this.listener = view.webview.onDidReceiveMessage(async (message) => {
      if (message.type === "refresh") await this.refresh();
      if (message.type === "prompt" && typeof message.text === "string") {
        await vscode.commands.executeCommand("workbench.action.chat.open", { query: message.text, isPartialQuery: true });
      }
    });
    this.refresh();
  }
  async refresh() {
    if (this.refreshing) return;
    this.refreshing = true;
    try {
      const data = await readDashboard();
      await this.view.webview.postMessage(data);
    } finally {
      this.refreshing = false;
    }
  }
  dispose() { this.listener?.dispose(); }
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
  register("focusDashboard", () => vscode.commands.executeCommand("workbench.action.openView", "cully.dashboard"));
  context.subscriptions.push(vscode.window.registerWebviewViewProvider("cully.dashboard", {
    resolveWebviewView(view) {
      dashboard?.dispose();
      dashboard = new CullyDashboard(view);
      view.onDidDispose(() => { dashboard?.dispose(); dashboard = undefined; });
    },
  }, { webviewOptions: { retainContextWhenHidden: true } }));
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
