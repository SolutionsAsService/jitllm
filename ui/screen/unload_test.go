package screen

import (
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"

	"github.com/jitllm/jitllm/ui/app"
	"github.com/jitllm/jitllm/ui/widgets"
)

// unloadButtons is every Unload button in a tree, in order.
func unloadButtons(w widget.Widget, out *[]*widgets.IconButton) {
	if b, ok := w.(*widgets.IconButton); ok && b.Label() == "Unload" {
		*out = append(*out, b)
	}
	for _, c := range w.Children() {
		unloadButtons(c, out)
	}
}

// Every loaded model has an Unload button, and it closes that model in the
// engine: unloading is the only way to give a model's memory back, and the
// window's hints tell a person to do it.
func TestEveryLoadedModelCanBeUnloaded(t *testing.T) {
	sh := app.NewShell(nil, nil)
	var closed []string
	Attach(sh, Deps{Engine: &fakeEngine{unload: func(p string) { closed = append(closed, p) }}})
	sh.Store.Models.Set([]app.LoadedModel{
		{Path: "/m/a.jlm", Name: "a", Active: true},
		{Path: "/m/b.jlm", Name: "b"},
	})
	panel := openModels(sh)
	panel.Layout(mockCtx(), geometry.Loose(geometry.Sz(900, 600)))

	var bs []*widgets.IconButton
	unloadButtons(panel, &bs)
	if len(bs) != 2 {
		t.Fatalf("%d Unload buttons for 2 loaded models", len(bs))
	}
	b := bs[1]
	at := b.Bounds().Center()
	b.Event(nil, &event.MouseEvent{MouseType: event.MousePress, Button: event.ButtonLeft, Position: at})
	b.Event(nil, &event.MouseEvent{MouseType: event.MouseRelease, Button: event.ButtonLeft, Position: at})
	if len(closed) != 1 || closed[0] != "/m/b.jlm" {
		t.Fatalf("Unload on the second row closed %v, want [/m/b.jlm]", closed)
	}
}
