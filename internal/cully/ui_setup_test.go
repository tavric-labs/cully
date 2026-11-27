package cully

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSetupPageIsWired(t *testing.T) {
	app, err := uiStaticFS.ReadFile("ui_static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: 'setup'", "'#/setup'", "/api/setup/state", "/api/setup/rules/approve", "Welcome and check", "Choose a mode", "Rules interview", "Review and approve", "What Cully could do for you", "Approve and link to my harnesses", "Setup is not finished."} {
		if !strings.Contains(string(app), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	html, _ := uiStaticFS.ReadFile("ui_static/index.html")
	for _, want := range []string{`src="/setup.js"`, `href="#/setup"`, `id="view-setup"`} {
		if !strings.Contains(string(html), want) {
			t.Errorf("index.html lacks %q", want)
		}
	}
	if strings.Index(string(html), "/setup.js") > strings.Index(string(html), "/app.js") {
		t.Error("setup.js must load before app.js")
	}
}

// The setup helpers are plain JavaScript with no DOM; run them with node when it is installed.
func TestSetupHelpersInNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	js, err := uiStaticFS.ReadFile("ui_static/setup.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(js) + `
const W = module.exports || CullySetup;
const assert = require('assert');
const same = (n) => Array.from({ length: n }, (_, i) => ({ t: 'same', l: 'l' + i }));
let rows = W.collapseDiff([...same(10), { t: 'add', l: 'new' }, { t: 'del', l: 'old' }, ...same(10)], 2);
assert.deepStrictEqual(rows.map((r) => r.t), ['fold', 'same', 'same', 'add', 'del', 'same', 'same', 'fold']);
assert.strictEqual(rows[0].n, 8);
assert.deepStrictEqual(W.collapseDiff(same(5)).map((r) => r.t), ['fold']);
assert.deepStrictEqual(W.collapseDiff(undefined), []);
assert.strictEqual(W.diffSentence([{ t: 'add' }, { t: 'add' }, { t: 'del' }]), '2 lines added, 1 line removed');
assert.strictEqual(W.diffSentence(same(3)), 'No changes to your rules.');
assert.strictEqual(W.progress(2, 10), 'Question 3 of 10');
assert.strictEqual(W.progress(99, 4), 'Question 4 of 4');
assert.strictEqual(W.progress(0, 0), 'No questions');
const qs = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
assert.deepStrictEqual(W.tally(qs, { a: 'x', b: null }), { answered: 1, skipped: 1, left: 1 });
const bars = W.barScale({ claude: { hours: 60 }, codex: { hours: 3 }, cursor: { hours: 0 } }, 'hours', ['claude', 'codex', 'cursor']);
assert.deepStrictEqual(bars.map((b) => b.pct), [100, 5, 0]);
assert.strictEqual(W.barScale({ codex: { hours: 0.01 }, claude: { hours: 100 } }, 'hours', ['codex'])[0].pct, 100);
assert.strictEqual(W.tokens(31e6), '31M'); assert.strictEqual(W.tokens(40000), '40k'); assert.strictEqual(W.tokens(1500000), '1.5M'); assert.strictEqual(W.tokens(12), '12');
assert.strictEqual(W.hours(89), '89 h'); assert.strictEqual(W.hours(0.5), '30 min'); assert.strictEqual(W.hours(2.5), '2.5 h');
assert.strictEqual(W.basisSentence({ sessions: 212, days: 30, hours: 89, tokens: 31e6 }), 'From 212 sessions over 30 days, about 89 h and 31M tokens');
assert.strictEqual(W.basisSentence({}), 'No sessions found yet.');
assert.strictEqual(W.saves(25), 'saves about 25 min a week'); assert.strictEqual(W.saves(0), '');
assert.strictEqual(W.costs(40000), 'costs about 40k tokens a run');
assert.ok(W.enough({ sessions: 20 }) && !W.enough({ sessions: 19 }) && !W.enough());
assert.strictEqual(W.stepDone('mode', { steps: [{ id: 'mode', done: true }] }), true);
assert.strictEqual(W.stepDone('mode', {}), false);
console.log('ok');
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
