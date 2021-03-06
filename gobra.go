/*
MIT License

Copyright (c) 2017 Chris Tessum

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

// Package gobra is an HTML-based graphical user interface (GUI) for the cobra
// command line interface (CLI; github.com/spf13/cobra).
package gobra

import (
	"bytes"
	"strings"
	"fmt"
	"net/http"
	"html/template"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandFromCobra is a wrapper for cobra.Command to work with Gobra
type CommandFromCobra struct {
	// CobraCmd holds the pointer to a Cobra command
	CobraCmd *cobra.Command 
	// Server address that the front-end will communicate with.
	// If left blank, it will communicate to /gobra of the same host.
	ServerAddress string
}

var tCmd *template.Template

// convert pflag.FlagSet to slices for range to iterate
func FlagSetToSlice(fl *pflag.FlagSet) []*pflag.Flag {
	var out []*pflag.Flag

	fl.VisitAll(func (f *pflag.Flag) {
		out = append(out, f)
	})
	return out
}

func init() {

	var funcMaps = template.FuncMap{
		"flagSetToSlice" : FlagSetToSlice,
	}

	const commandTpl = `
<div id="gobra-{{.CobraCmd.Use}}">
{{ define "command" }}
	<div data-gobra-name={{.Use}} style="{{if .HasParent }}display:none;{{end}}">
		<h3>{{.Use}}</h3>
		<ul class="flags">
			{{ range (flagSetToSlice .PersistentFlags) }}
				<li><code>--{{ .Name }}="{{ .Value.String }}"</code> {{ .Usage }} </li>
			{{ end }}
			{{ range (flagSetToSlice .Flags) }}
				<li><code data-name={{ .Name }}>--{{ .Name }}=<input value={{ .Value.String }}></input></code> {{ .Usage }} </li>
			{{ end }}
		</ul>
		{{ if .HasSubCommands }}
			<select data-gobra-select>
				<option selected disabled>Select</option>
				{{ range .Commands }}
				<option value="{{.Use}}">{{ .Use }}</option>
				{{ end }}
			</select>
			{{range .Commands }}
				{{ template "command" .}}
			{{ end }}
		{{ end }}
	</div>
{{ end }}
{{ template "command" .CobraCmd }}
<br/>
<button>Execute</button>
<pre class="gobraStatus" style="padding:10px; background:lightgray; height:10em; overflow-y:scroll; white-space: pre-wrap;">
</pre>
<script>
{{ with .CobraCmd }}
let status = document.querySelector("#gobra-{{.Use}} .gobraStatus");
	document.querySelectorAll("#gobra-{{.Use}} [data-gobra-select]").forEach( option => {
		option.onchange = e => {
			[...e.target.parentElement.children].forEach( el => {
				if (el.tagName == "DIV")
					el.style.display = (el.dataset.gobraName == e.target.value)? "" : "none";
			})
		}
	});

	document.querySelector("#gobra-{{.Use}}>button").onclick = e => {
		let recurse = el => {
			let cmds = [],
				flags = [];
			if (el.tagName = "DIV" && el.style.display !== "none") {
				if (el.dataset.gobraName) { 
					cmds.push(el.dataset.gobraName);
					[...el.querySelector("ul.flags").querySelectorAll("code")].forEach(f => {
						if(f.children[0]) flags.push(f.dataset.name + "=" + f.children[0].value);
					})
				}
				[...el.children].forEach( child => {
					if (child.style.display !== "none") {
						let childRes = recurse(child);
						Array.prototype.push.apply(cmds, childRes[0]);
						Array.prototype.push.apply(flags, childRes[1]);
						return;
					} 
				})
			}
			return [cmds, flags];
		}
		let resultCmd = recurse(document.getElementById("gobra-{{.Use}}"));

		status.textContent += "→ "+resultCmd.reduce((x,y) => {
				return x.join(" ") + " " 
					+ y.map(z => 
						"--"+z.split("=")[0]+"=\""+z.split("=")[1]+"\""
					).join(" ")
			})+"\n" ;
		status.scrollTop = status.scrollHeight;

		serverSend(resultCmd[0], resultCmd[1])
		.then(res => res.text()).then( d => {
			status.textContent += "← " + d + "\n";
			status.scrollTop = status.scrollHeight;
		}).catch(e => {
			status.textContent += "⤬ Failed communicating with server: " + e + "\n";
			status.scrollTop = status.scrollHeight;
		})
	}
{{ end }}
const serverAddress = {{ if .ServerAddress}} {{ .ServerAddress }} {{ else }} "/" {{ end }};
let serverSend = (cmds, flags) => {
	return fetch(cmds.join("/")+"?"+flags.join("&"));
}
</script>
</div>
`

	tCmd = template.Must(template.New("commands").Funcs(funcMaps).Parse(commandTpl))
}

// Render renders the view of the command.
func (c *CommandFromCobra) Render() ([]byte, error) {
	b := new(bytes.Buffer)
	if err := tCmd.Execute(b, c); err != nil {
		return b.Bytes(), err
	}
	
	return b.Bytes(), nil
}

// Server-side
// Serves a Gobra API at: <hostname>:<port>/
// Also serves a front-end interface at: <hostname>:<port>/[index.html]
// You must generate this interface first with gobra.CommandFromCobra.Render(), or it will serve 404

// Server struct/class that holds configuration for a Cobra back-end instance
type Server struct {
	// Gobra command tree root
	Root *cobra.Command
	// port the server will run on
	Port int
	// Allow Cross-Origin. If set to true, everyone can use the Gobra instance on client-side
	// Set this to true if you're planning to expose the API to public.
	AllowCORS bool
	// If set to true, it won't be serving an html for front-end
	Frontless bool
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	if (r.URL.Path == "/") {
		// Serves front-end if root is requested
		if !s.Frontless {
			http.ServeFile(w, r, "index.html")
		}
	} else if (strings.HasPrefix(r.URL.Path, "/"+s.Root.Use)) {
		// Serves API if path starts with root command name
		if s.AllowCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		r.ParseForm()
		cmds := strings.Split(r.URL.Path[1:], "/")
		flags := r.Form

		var out bytes.Buffer
		s.Root.SetArgs(cmds[1:])
		s.Root.SetOutput(&out)

		// Getting the command we need to set flags
		c, _, _ := s.Root.Find(cmds[1:])
		for key, values := range flags {
			c.Flags().Set(key, values[0])
		}
		
		fmt.Println("Executing: ", cmds, flags)
		s.Root.ExecuteC()
		fmt.Fprintf(w, out.String())

	} else {
		// Everything else gets a 404
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "404 Page not Found")
	}
}

func (s *Server) Start() {
	http.HandleFunc("/", s.handler)
	fmt.Println(http.ListenAndServe(":" + fmt.Sprintf("%v", s.Port), nil))
}
