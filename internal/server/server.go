package server

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

//go:embed static/index.html
var indexHTML string

// NewMux 建立 dashboard 的 HTTP handler（方便測試）
func NewMux(ws *protocol.Workspace) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		handleTasks(ws, w)
	})
	mux.HandleFunc("/api/messages/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/messages/")
		handleMessages(ws, featureID, w)
	})
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/api/events/")
		handleEvents(ws, featureID, w)
	})
	mux.HandleFunc("/sse/events/", func(w http.ResponseWriter, r *http.Request) {
		featureID := strings.TrimPrefix(r.URL.Path, "/sse/events/")
		handleSSE(ws, featureID, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, indexHTML)
	})

	return mux
}

// Start 啟動 dashboard web server
func Start(ws *protocol.Workspace, port int) error {
	return http.ListenAndServe(fmt.Sprintf(":%d", port), NewMux(ws))
}

type taskInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Phase   string `json:"phase"`
	Role    string `json:"role"`
	Round   int    `json:"round"`
	Active  bool   `json:"active"`
	Runner  string `json:"runner"`
}

func handleTasks(ws *protocol.Workspace, w http.ResponseWriter) {
	features, err := ws.ListFeatures()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var tasks []taskInfo
	for _, f := range features {
		t := taskInfo{
			ID:     f.ID,
			Name:   f.Name,
			Status: f.Status,
		}
		if s, err := ws.ReadState(f.ID); err == nil {
			t.Phase = string(s.Phase)
			t.Role = string(s.Role)
			t.Round = s.Round
			t.Active = s.Active
			t.Runner = s.Runner
		}
		tasks = append(tasks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

type messageInfo struct {
	Role    string `json:"role"`
	Label   string `json:"label"`
	Content string `json:"content"`
	File    string `json:"file"`
	Round   int    `json:"round,omitempty"`
}

func handleMessages(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	dir := ws.FeatureDir(featureID)
	var messages []messageInfo

	for _, f := range []struct {
		name string
		role string
	}{
		{protocol.TaskBrief, "designer"},
		{protocol.Criteria, "designer"},
	} {
		content := readIfExists(filepath.Join(dir, f.name))
		if content != "" {
			messages = append(messages, messageInfo{
				Role:    f.role,
				Label:   f.name,
				Content: content,
				File:    f.name,
			})
		}
	}

	roundsDir := filepath.Join(dir, protocol.RoundsDir)
	entries, _ := os.ReadDir(roundsDir)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "round-") {
			continue
		}
		roundNum := 0
		fmt.Sscanf(entry.Name(), "round-%d", &roundNum)
		roundPath := filepath.Join(roundsDir, entry.Name())

		for _, f := range []struct {
			name string
			role string
		}{
			{"coder-report.md", "coder"},
			{"review-report.md", "reviewer"},
			{"deep-review-report.md", "deep-reviewer"},
			{"test-report.md", "tester"},
			{"web-test-report.md", "tester"},
			{"gate-test-report.md", "tester"},
		} {
			content := readIfExists(filepath.Join(roundPath, f.name))
			if content != "" {
				messages = append(messages, messageInfo{
					Role:    f.role,
					Label:   f.name,
					Content: content,
					File:    filepath.Join(entry.Name(), f.name),
					Round:   roundNum,
				})
			}
		}
	}

	final := readIfExists(filepath.Join(dir, protocol.FinalReport))
	if final != "" {
		messages = append(messages, messageInfo{
			Role:    "acceptor",
			Label:   protocol.FinalReport,
			Content: final,
			File:    protocol.FinalReport,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func handleEvents(ws *protocol.Workspace, featureID string, w http.ResponseWriter) {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	content := readIfExists(path)
	if content == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "[]")
		return
	}

	var events []json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line != "" {
			events = append(events, json.RawMessage(line))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// handleSSE 用 polling 方式 tail events.jsonl 並以 SSE 推送
func handleSSE(ws *protocol.Workspace, featureID string, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	var lastOffset int64

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.Size() <= lastOffset {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if lastOffset > 0 {
				f.Seek(lastOffset, 0)
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			lastOffset = info.Size()
			f.Close()
			flusher.Flush()
		}
	}
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

const _oldIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>4x Live</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<script src="https://cdn.tailwindcss.com"></script>
<style>
  body { background: #0a0a0a; color: #e5e5e5; font-family: ui-monospace, monospace; }
  .role-designer { border-left: 3px solid #a855f7; }
  .role-coder { border-left: 3px solid #06b6d4; }
  .role-reviewer { border-left: 3px solid #22c55e; }
  .role-deep-reviewer { border-left: 3px solid #22c55e; }
  .role-tester { border-left: 3px solid #f97316; }
  .role-acceptor { border-left: 3px solid #eab308; }
</style>
</head>
<body class="min-h-screen">
<div class="flex h-screen">
  <!-- Sidebar -->
  <div id="sidebar" class="w-72 border-r border-zinc-800 overflow-y-auto p-4">
    <h1 class="text-lg font-bold mb-4">4x Live</h1>
    <div id="task-list"></div>
  </div>
  <!-- Main -->
  <div class="flex-1 overflow-y-auto p-6" id="main">
    <div id="empty" class="text-zinc-500 mt-20 text-center">Select a feature to view</div>
    <div id="messages" class="space-y-4 hidden"></div>
  </div>
</div>
<script>
let current = null;
const ROLES = {
  designer: {name:'Designer',color:'#a855f7'},
  coder: {name:'Coder',color:'#06b6d4'},
  reviewer: {name:'Reviewer',color:'#22c55e'},
  'deep-reviewer': {name:'Deep Review',color:'#22c55e'},
  tester: {name:'Tester',color:'#f97316'},
  acceptor: {name:'Acceptor',color:'#eab308'},
};
async function load() {
  const tasks = await (await fetch('/api/tasks')).json();
  const list = document.getElementById('task-list');
  list.innerHTML = '';
  const sorted = (tasks||[]).slice().sort((a,b) => {
    if(a.active && !b.active) return -1;
    if(!a.active && b.active) return 1;
    const order = {'in-progress':0,'not-started':1,'done':2};
    return (order[a.status]??1) - (order[b.status]??1);
  });
  sorted.forEach(t => {
    const el = document.createElement('div');
    const isActive = t.active && t.phase && t.phase!=='done';
    const isSel = t.id===current;
    let cls = 'p-3 rounded cursor-pointer mb-1 border ';
    if(isActive) cls += 'border-emerald-500/50 bg-emerald-950/30 ';
    else if(isSel) cls += 'border-zinc-600 bg-zinc-800 ';
    else if(t.status==='done') cls += 'border-transparent opacity-50 hover:opacity-80 ';
    else cls += 'border-transparent hover:bg-zinc-800 ';
    el.className = cls;
    const badge = isActive ? '<span class="inline-block px-1.5 py-0.5 text-[10px] font-bold bg-emerald-500 text-black rounded ml-2 animate-pulse">RUNNING</span>' : t.status==='done' ? '<span class="inline-block px-1.5 py-0.5 text-[10px] text-zinc-500 border border-zinc-700 rounded ml-2">DONE</span>' : '';
    const phase = isActive ? '<div class="text-xs text-emerald-400 mt-1">▶ '+t.phase+' · round '+(t.round||0)+'</div>' : '';
    el.innerHTML = '<div class="font-medium text-sm">'+t.name+badge+'</div><div class="text-xs text-zinc-500">'+t.id+'</div>'+phase;
    el.onclick = () => { current=t.id; load(); loadMessages(t.id); };
    list.appendChild(el);
  });
}
async function loadMessages(id) {
  document.getElementById('empty').classList.add('hidden');
  const el = document.getElementById('messages');
  el.classList.remove('hidden');
  const msgs = await (await fetch('/api/messages/'+id)).json();
  el.innerHTML = '';
  (msgs||[]).forEach(m => {
    const r = ROLES[m.role]||{name:m.role,color:'#888'};
    const div = document.createElement('div');
    div.className = 'role-'+m.role+' pl-4 py-3';
    div.innerHTML = '<div class="text-xs mb-1"><span style="color:'+r.color+'">'+r.name+'</span> <span class="text-zinc-600">'+m.label+(m.round?' · round '+m.round:'')+'</span></div><pre class="text-sm text-zinc-300 whitespace-pre-wrap">'+esc(m.content.slice(0,2000))+(m.content.length>2000?'\n...':'')+'</pre>';
    el.appendChild(div);
  });
}
function esc(s){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;');}
load(); setInterval(load, 5000);
</script>
</body>
</html>`
