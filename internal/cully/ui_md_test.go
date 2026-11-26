package cully

import (
	"strings"
	"testing"
)

func TestMarkdownRendererIsServedAndBuildsDOMOnly(t *testing.T) {
	md, err := uiStaticFS.ReadFile("ui_static/md.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(string(md), bad) {
			t.Errorf("md.js uses %s", bad)
		}
	}
	for _, want := range []string{"md-table", "codeblock", "mailto", "noopener", "task"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("md.js lacks %q", want)
		}
	}
	idx, _ := uiStaticFS.ReadFile("ui_static/index.html")
	if !strings.Contains(string(idx), `src="/md.js"`) {
		t.Error("index.html does not load md.js")
	}
	app, _ := uiStaticFS.ReadFile("ui_static/app.js")
	if !strings.Contains(string(app), "CullyMD.render") {
		t.Error("app.js does not use the renderer")
	}
}
