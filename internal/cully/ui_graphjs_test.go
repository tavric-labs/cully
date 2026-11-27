package cully

import (
	"os/exec"
	"strings"
	"testing"
)

// The layout is plain JavaScript with no DOM; run it with node when node is installed.
func TestGraphLayoutInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	js, err := uiStaticFS.ReadFile("ui_static/graph.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(js) + `
const G = module.exports || CullyGraph;
const assert = require('assert');
const mk = (id, phase, extra) => Object.assign({ id, phase, session: 's1', tags: phase === 'tool' ? ['tool:Bash'] : ['agent'], start: '2026-01-01T00:00:00Z', end: '2026-01-01T00:00:10Z', cause: 'x' }, extra || {});
// Fixed width, time order on x, one lane per session.
let nodes = [mk('a', 'preparing'), mk('b', 'working'), mk('c', 'needs-you', { session: 's2', end: '2026-01-01T00:03:10Z', start: '2026-01-01T00:00:10Z', tags: ['user', 'permission'] })];
let L = G.layout({ lanes: [{ id: 's1', label: 'claude' }, { id: 's2', label: 'codex' }], nodes });
assert.strictEqual(L.nodes.length, 3);
assert.ok(L.nodes[0].x < L.nodes[1].x && L.nodes[1].x < L.nodes[2].x);
assert.strictEqual(L.nodes[0].w, L.nodes[2].w);
assert.ok(L.nodes[2].y > L.nodes[0].y, 'second session is a lower lane');
assert.strictEqual(L.edges.length, 2);
assert.strictEqual(L.edges[1].cross, true);
assert.ok(/^M[\d. -]+ C/.test(L.edges[0].d));
assert.strictEqual(G.headline({ nodes }, Date.parse('2026-01-01T00:10:00Z')), '3 steps, 2 sessions, 1 waited on you for 3 min');
// A long retry loop folds into one pair with a counter; a short one does not.
const loop = (n) => { const a = [mk('s', 'preparing')]; for (let i = 0; i < n; i++) { a.push(mk('w' + i, 'working')); a.push(mk('t' + i, 'tool')); } a.push(mk('z', 'working')); a.push(mk('end', 'done')); return a; };
L = G.layout({ lanes: [], nodes: loop(7) });
assert.strictEqual(L.back.length, 1); assert.strictEqual(L.back[0].label, 'x7'); assert.ok(L.hidden > 0);
L = G.layout({ lanes: [], nodes: loop(3) });
assert.strictEqual(L.back.length, 0); assert.strictEqual(L.hidden, 0);
// A step before a failure is only a suspect.
L = G.layout({ lanes: [], nodes: [mk('a', 'working'), mk('b', 'tool'), mk('c', 'failed')] });
assert.strictEqual(L.nodes[1].suspected, true);
assert.ok(!L.nodes[0].suspected);
// A helper node goes to its own lane, which can be folded into the parent.
const hn = [mk('a', 'working'), mk('b', 'helper'), mk('c', 'working')];
assert.strictEqual(G.layout({ lanes: [], nodes: hn }).rows.length, 2);
const folded = G.layout({ lanes: [], nodes: hn }, { collapsed: new Set(['s1']) });
assert.strictEqual(folded.nodes[1].y, folded.nodes[0].y);
console.log('ok');
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
