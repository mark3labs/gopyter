// Package dom is gopyter's implementation of GoNB's gonbui/dom package:
// changes to HTML already displayed, by element id. gopyter keeps each
// cell's HTML output and redraws it; JavaScript can't run.
//
// Its API and documentation are adapted from GoNB's gonbui/dom package
// (https://github.com/janpfeifer/gonb/tree/main/gonbui/dom), by Jan
// Pfeifer, under the MIT license: see LICENSE in the enclosing gonb
// directory.
package dom

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"

	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui/protocol"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gopyter/nb/wire"
)

// errNoJS is returned by the functions that load JavaScript.
var errNoJS = errors.New("gopyter runs in a terminal and can't run JavaScript")

func op(action, target string, fields map[string]any) {
	if !gonbui.IsNotebook {
		return
	}
	m := map[string]any{"op": "dom", "action": action, "target": target}
	maps.Copy(m, fields)
	wire.Op(m)
}

// TransientJavascript would run JavaScript: gopyter ignores it.
func TransientJavascript(js string) {}

// CreateTransientDiv displays an empty <div> and returns its id, to add
// content (e.g. widgets) to it.
func CreateTransientDiv() (htmlId string) {
	uid := gonbui.UniqueId()
	htmlId = "dom.transient_div_" + uid
	gonbui.UpdateHtml("gonb_update_"+uid, fmt.Sprintf(`<div id="%s"></div>`, htmlId))
	return htmlId
}

// RelativePositionId is where InsertAdjacent inserts HTML.
type RelativePositionId string

// Positions for InsertAdjacent, as in JavaScript's insertAdjacentHTML.
const (
	BeforeBegin RelativePositionId = "beforebegin"
	AfterBegin  RelativePositionId = "afterbegin"
	BeforeEnd   RelativePositionId = "beforeend"
	AfterEnd    RelativePositionId = "afterend"
)

// InsertAdjacent inserts html next to (or inside) the element referenceId.
func InsertAdjacent(referenceId string, pos RelativePositionId, html string) {
	op("insert", referenceId, map[string]any{"pos": string(pos), "data": html})
}

// Append appends html to the contents of the element parentHtmlId.
func Append(parentHtmlId, html string) { InsertAdjacent(parentHtmlId, BeforeEnd, html) }

// SetInnerHtml replaces the contents of the element htmlId.
func SetInnerHtml(htmlId, html string) { op("set_html", htmlId, map[string]any{"data": html}) }

// SetInnerText replaces the contents of the element htmlId with text.
func SetInnerText(htmlId, text string) { op("set_text", htmlId, map[string]any{"data": text}) }

// GetInnerHtml returns the contents of the element htmlId.
func GetInnerHtml(htmlId string) (html string) {
	if !gonbui.IsNotebook {
		return ""
	}
	raw, ok := wire.Request(map[string]any{"op": "dom", "action": "get", "target": htmlId})
	if ok {
		_ = json.Unmarshal(raw, &html) // not a string: no contents
	}
	return html
}

// Remove removes the element htmlId.
func Remove(htmlId string) { op("remove", htmlId, nil) }

// Persist is kept for compatibility: gopyter saves every output.
func Persist(htmlId string) {}

// SendAsDownload would make the browser download data: gopyter writes it
// to fileName in the program's directory.
func SendAsDownload(fileName string, data []byte, mimeType protocol.MIMEType) {
	if err := os.WriteFile(fileName, data, 0o644); err != nil {
		gonbui.DisplayHtml(fmt.Sprintf("<b>download failed</b>: %v", err))
		return
	}
	gonbui.DisplayMarkdown(fmt.Sprintf("saved `%s` (%d bytes)", fileName, len(data)))
}

// LoadScriptModuleAndRun loads JavaScript: not supported by gopyter.
func LoadScriptModuleAndRun(src string, attributes map[string]string, runJS string) error {
	return errNoJS
}

// LoadScriptOrRequireJSModuleAndRun loads JavaScript: not supported by gopyter.
func LoadScriptOrRequireJSModuleAndRun(moduleName, src string, attributes map[string]string, runJS string) error {
	return errNoJS
}

// LoadScriptOrRequireJSModuleAndRunTransient loads JavaScript: not
// supported by gopyter.
func LoadScriptOrRequireJSModuleAndRunTransient(moduleName, src string, attributes map[string]string, runJS string) error {
	return errNoJS
}
