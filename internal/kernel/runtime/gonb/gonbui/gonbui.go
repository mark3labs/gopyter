// Package gonbui is gopyter's implementation of GoNB's gonbui package, so
// notebooks written for GoNB (https://github.com/janpfeifer/gonb) run in
// gopyter. HTML is drawn as text in the terminal; JavaScript can't run.
//
// Its API and documentation are adapted from GoNB's gonbui package
// (https://github.com/janpfeifer/gonb/tree/main/gonbui), by Jan
// Pfeifer, under the MIT license: see LICENSE in the enclosing gonb
// directory.
package gonbui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"log"

	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui/protocol"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gopyter/nb/wire"
)

// IsNotebook reports whether the program runs in a notebook (gopyter).
var IsNotebook = wire.Connected()

// Debug enables Logf.
var Debug bool

// OnCommValueUpdate is kept for compatibility; gopyter delivers values
// through package comms.
var OnCommValueUpdate func(valueMsg *protocol.CommValue)

// Logf logs to stderr when Debug is set.
func Logf(format string, args ...any) {
	if Debug {
		log.Printf(format, args...)
	}
}

// Error returns the error of the connection to the notebook: always nil.
func Error() error { return nil }

// Open connects to the notebook; gopyter is always connected.
func Open() error { return nil }

// Sync waits until the notebook processed everything sent so far.
func Sync() {
	if IsNotebook {
		_, _ = wire.Request(map[string]any{"op": "sync"}) // best effort: nothing to do if the UI is gone
	}
}

// UniqueId returns a new unique id, e.g. for UpdateHtml.
func UniqueId() string { return wire.NewID()[:8] }

// UniqueID is an alias for UniqueId.
func UniqueID() string { return UniqueId() }

func display(mime, data, id string) {
	if IsNotebook {
		wire.Display(mime, data, "", id)
	}
}

// SendData displays data, or delivers a front-end message (comm values,
// input requests).
func SendData(data *protocol.DisplayData) {
	if !IsNotebook || data == nil {
		return
	}
	d := data.Data
	switch v := d[protocol.MIMECommValue].(type) {
	case *protocol.CommValue:
		sendComm(v)
		return
	case protocol.CommValue:
		sendComm(&v)
		return
	}
	switch v := d[protocol.MIMEJupyterInput].(type) {
	case *protocol.InputRequest:
		RequestInput(v.Prompt, v.Password)
		return
	case protocol.InputRequest:
		RequestInput(v.Prompt, v.Password)
		return
	}
	if _, ok := d[protocol.MIMECommSubscribe]; ok {
		return // every value is delivered anyway
	}
	// Show the richest representation.
	if s, ok := d[protocol.MIMETextMarkdown].(string); ok {
		display("text/markdown", s, data.DisplayID)
		return
	}
	switch v := d[protocol.MIMEImagePNG].(type) {
	case []byte:
		display("image/png", base64.StdEncoding.EncodeToString(v), data.DisplayID)
		return
	case string:
		display("image/png", v, data.DisplayID)
		return
	}
	if s, ok := d[protocol.MIMETextHTML].(string); ok {
		display("text/html", s, data.DisplayID)
		return
	}
	if s, ok := d[protocol.MIMEImageSVG].(string); ok {
		display("text/html", "<div>"+s+"</div>", data.DisplayID)
		return
	}
	if s, ok := d[protocol.MIMETextPlain].(string); ok {
		display("text/plain", s, data.DisplayID)
	}
}

func sendComm(v *protocol.CommValue) {
	if v.Request {
		wire.Op(map[string]any{"op": "read", "address": v.Address})
		return
	}
	wire.Op(map[string]any{"op": "value", "address": v.Address, "value": v.Value})
}

// DisplayHtml is an alias for DisplayHTML.
func DisplayHtml(html string) { DisplayHTML(html) }

// DisplayHTML displays HTML in the cell's output. gopyter draws it as text:
// formatting, lists, tables, images and widgets are shown; styles and
// scripts are ignored.
func DisplayHTML(html string) { display("text/html", html, "") }

// DisplayHTMLF is DisplayHTML with fmt.Sprintf formatting.
func DisplayHTMLF(htmlFormat string, args ...any) { DisplayHTML(fmt.Sprintf(htmlFormat, args...)) }

// DisplayHtmlf is an alias for DisplayHTMLF.
func DisplayHtmlf(htmlFormat string, args ...any) { DisplayHTMLF(htmlFormat, args...) }

// DisplayMarkdown displays markdown in the cell's output.
func DisplayMarkdown(markdown string) { display("text/markdown", markdown, "") }

// UpdateHTML displays HTML in an output block identified by id: created
// the first time, replaced afterwards.
func UpdateHTML(id, html string) { display("text/html", html, id) }

// UpdateHtml is an alias for UpdateHTML.
func UpdateHtml(id, html string) { UpdateHTML(id, html) }

// UpdateMarkdown is UpdateHTML for markdown.
func UpdateMarkdown(id, markdown string) { display("text/markdown", markdown, id) }

// DisplayPng displays PNG data.
func DisplayPng(png []byte) {
	display("image/png", base64.StdEncoding.EncodeToString(png), "")
}

// DisplayPNG is an alias for DisplayPng.
func DisplayPNG(png []byte) { DisplayPng(png) }

// DisplayImage displays an image, encoded as PNG.
func DisplayImage(image image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image); err != nil {
		return err
	}
	DisplayPng(buf.Bytes())
	return nil
}

// DisplaySvg displays SVG, embedded in HTML as GoNB does. gopyter can't
// draw SVG in the terminal and shows a placeholder, but saves it in the
// notebook for Jupyter.
func DisplaySvg(svg string) { DisplayHtml("<div>" + svg + "</div>") }

// DisplaySVG is an alias for DisplaySvg.
func DisplaySVG(svg string) { DisplaySvg(svg) }

// RequestInput asks the user to type a line for the program's stdin, with
// prompt in front of the field. With password, the input isn't shown.
func RequestInput(prompt string, password bool) {
	if IsNotebook {
		wire.Op(map[string]any{"op": "input", "prompt": prompt, "password": password})
	}
}

// EmbedImageAsPNGSrc returns img as a data URL for an <img> src.
func EmbedImageAsPNGSrc(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("failed to encode image as PNG: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ScriptJavascript would run JavaScript in the front-end: gopyter has
// none, so it's ignored.
func ScriptJavascript(js string) {}
