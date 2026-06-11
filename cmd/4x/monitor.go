package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/server"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "monitor [path...]",
		Short: "Monitor multiple 4x projects from a single dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				args = []string{cwd}
			}

			var workspaces []*protocol.Workspace
			for _, path := range args {
				ws, err := protocol.Find(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: %s — %v\n", path, err)
					continue
				}
				workspaces = append(workspaces, ws)
				fmt.Printf("  + %s (%s)\n", ws.Root, path)
			}

			if len(workspaces) == 0 {
				return fmt.Errorf("no valid 4x projects found")
			}

			if len(workspaces) == 1 {
				fmt.Printf("\n4x Monitor — http://localhost:%d\n", port)
				return server.Start(workspaces[0], port)
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
				handleProjects(workspaces, w)
			})
			for i, ws := range workspaces {
				ws := ws
				prefix := fmt.Sprintf("/api/project/%d", i)
				mux.Handle(prefix+"/", http.StripPrefix(prefix, server.NewMux(ws)))
			}
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, monitorHTML)
			})

			fmt.Printf("\n4x Monitor — http://localhost:%d (%d projects)\n", port, len(workspaces))
			return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
		},
	}

	cmd.Flags().IntVar(&port, "port", 4567, "dashboard port")
	return cmd
}

type projectInfo struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

func handleProjects(workspaces []*protocol.Workspace, w http.ResponseWriter) {
	var projects []projectInfo
	for i, ws := range workspaces {
		cfg, _ := ws.ReadConfig()
		projects = append(projects, projectInfo{
			Index: i,
			Name:  cfg.Project.Name,
			Path:  ws.Root,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

const monitorHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>4x Monitor</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<script src="https://cdn.tailwindcss.com"></script>
<style>body{background:#0a0a0a;color:#e5e5e5;font-family:ui-monospace,monospace}</style>
</head>
<body class="min-h-screen p-6">
<h1 class="text-xl font-bold mb-4">4x Monitor</h1>
<div id="projects" class="space-y-6"></div>
<script>
async function load(){
  const projects=await(await fetch('/api/projects')).json();
  const el=document.getElementById('projects');
  el.innerHTML='';
  for(const p of (projects||[])){
    const tasks=await(await fetch('/api/project/'+p.index+'/api/tasks')).json();
    const div=document.createElement('div');
    div.className='border border-zinc-800 rounded p-4';
    div.innerHTML='<h2 class="font-bold text-lg mb-2">'+p.name+' <span class="text-zinc-500 text-sm">'+p.path+'</span></h2>';
    const table=document.createElement('div');
    table.className='space-y-1 text-sm';
    (tasks||[]).forEach(t=>{
      const s=t.active?'🟢':'';
      table.innerHTML+='<div class="flex gap-4"><span class="w-48 text-zinc-400">'+t.id+'</span><span class="w-32">'+(t.phase||t.status)+'</span><span>'+s+'</span></div>';
    });
    div.appendChild(table);
    el.appendChild(div);
  }
}
load();setInterval(load,5000);
</script>
</body>
</html>`
