package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// statusHTML is the single-page status UI: no build step, no assets, polls
// /status.json every 2s. Terminal aesthetic to match jamesponwith.github.io.
const statusHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>llm-resiliency-router</title>
<style>
body{background:#0b0e0c;color:#c7d0c9;font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;max-width:900px;margin:2rem auto;padding:0 1rem}
h1{color:#7ee787;font-size:16px;font-weight:600}h1::before{content:"$ ";color:#8b949e}
table{border-collapse:collapse;width:100%;margin:0 0 1.5rem}
td,th{text-align:left;padding:.3rem .8rem;border-bottom:1px solid #1c231e}
th{color:#8b949e;font-weight:400}
.healthy{color:#7ee787}.degraded{color:#e3b341}.ejected{color:#f85149}.half-open{color:#79c0ff}
#d div{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;color:#8b949e}
.mode{color:#e3b341}a{color:#79c0ff}
footer{margin-top:1.5rem;color:#30363d}
</style>
<h1>llm-resiliency-router <span class="mode" id="m"></span></h1>
<table><thead><tr><th>upstream</th><th>kind</th><th>cell</th></tr></thead><tbody id="u"></tbody></table>
<h1>recent decisions</h1><div id="d"></div>
<footer><a href="/metrics">/metrics</a> · <a href="/status.json">/status.json</a></footer>
<script>
async function tick(){
  var s = await (await fetch("/status.json")).json();
  document.getElementById("m").textContent = "[" + s.mode + "]";
  document.getElementById("u").innerHTML = s.upstreams.map(function(u){
    return "<tr><td>"+u.name+"</td><td>"+u.kind+"</td><td class='"+u.state+"'>"+u.state+"</td></tr>";
  }).join("");
  document.getElementById("d").innerHTML = (s.decisions||[]).map(function(d){
    return "<div>"+d.ts.slice(11,19)+"  "+d.path+" → "+(d.chose||"-")+"  "+d.status+"  "+
      d.dur_ms+"ms  "+(d.events||[]).join(" ")+"</div>";
  }).join("");
}
tick(); setInterval(tick, 2000);
</script>`

func (rt *router) statusJSON(w http.ResponseWriter, _ *http.Request) {
	type up struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		State string `json:"state"`
	}
	ups := make([]up, len(rt.cfg.Upstreams))
	for i, u := range rt.cfg.Upstreams {
		ups[i] = up{u.Name, u.Kind, rt.cells[i].State().String()}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"mode": rt.cfg.Mode, "upstreams": ups, "decisions": rt.dlog.last(),
	})
}

func (rt *router) statusPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, statusHTML)
}
