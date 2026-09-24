// Package widgets is gopyter's implementation of GoNB's gonbui/widgets
// package: buttons, sliders and selects in the cell's output, drawn by
// gopyter in the terminal. Use them with the keyboard (tab to move
// between them) or the mouse while the cell runs.
//
//	slider := widgets.Slider(0, 100, 50).Done()
//	for v := range slider.Listen().C {
//		fmt.Println(v)
//	}
//
// When the user presses Done, Listen channels are closed, so such loops
// end.
//
// Its API and documentation are adapted from GoNB's gonbui/widgets package
// (https://github.com/janpfeifer/gonb/tree/main/gonbui/widgets), by Jan
// Pfeifer, under the MIT license: see LICENSE in the enclosing gonb
// directory.
package widgets

import (
	"fmt"
	"html"
	"strings"
	"sync/atomic"

	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui/comms"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui/dom"
)

func panicf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }

// show displays a widget's HTML, in parent if set.
func show(parent, html string) {
	if parent == "" {
		gonbui.DisplayHtml(html)
	} else {
		dom.Append(parent, html)
	}
}

// ButtonBuilder builds a button.
type ButtonBuilder struct {
	address, label, htmlId, parentHtmlId string
	built                                bool
}

// Button returns a builder for a button with label. Listen receives a
// counter incremented at each click.
func Button(label string) *ButtonBuilder {
	return &ButtonBuilder{
		label:   label,
		address: "/button/" + gonbui.UniqueId(),
		htmlId:  "gonb_button_" + gonbui.UniqueId(),
	}
}

func (b *ButtonBuilder) check() {
	if b.built {
		panicf("ButtonBuilder cannot change parameters after it is built")
	}
}

// WithHtmlId sets the id of the button's element.
func (b *ButtonBuilder) WithHtmlId(htmlId string) *ButtonBuilder {
	b.check()
	b.htmlId = htmlId
	return b
}

// WithAddress sets the address the button's clicks are sent to.
func (b *ButtonBuilder) WithAddress(address string) *ButtonBuilder {
	b.check()
	b.address = address
	return b
}

// AppendTo puts the button in the element parentHtmlId (see dom).
func (b *ButtonBuilder) AppendTo(parentHtmlId string) *ButtonBuilder {
	b.check()
	b.parentHtmlId = parentHtmlId
	return b
}

// Done displays the button.
func (b *ButtonBuilder) Done() *ButtonBuilder {
	if b.built {
		panicf("ButtonBuilder.Done already called!?")
	}
	b.built = true
	show(b.parentHtmlId, fmt.Sprintf(`<button id="%s" type="button" data-address="%s">%s</button>`,
		html.EscapeString(b.htmlId), html.EscapeString(b.address), b.label))
	return b
}

// Listen returns a channel receiving the click counter at each click.
func (b *ButtonBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("ButtonBuilder.Listen can only be called after the button was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the id of the button's element.
func (b *ButtonBuilder) HtmlId() string { return b.htmlId }

// Address returns the address of the button's clicks.
func (b *ButtonBuilder) Address() string { return b.address }

// value tracks the value of a slider or select.
type value struct {
	address string
	v       atomic.Int64
}

func (v *value) track() {
	comms.Subscribe(v.address, func(_ string, n int) { v.v.Store(int64(n)) })
}

// SliderBuilder builds a slider.
type SliderBuilder struct {
	value
	htmlId, parentHtmlId string
	min, max             int
	built                bool
}

// Slider returns a builder for a slider from min to max, at value.
func Slider(min, max, value int) *SliderBuilder {
	b := &SliderBuilder{min: min, max: max, htmlId: "gonb_slider_" + gonbui.UniqueId()}
	b.address = "/slider/" + gonbui.UniqueId()
	b.v.Store(int64(value))
	return b
}

func (b *SliderBuilder) check() {
	if b.built {
		panicf("SliderBuilder cannot change parameters after it is built")
	}
}

// WithHtmlId sets the id of the slider's element.
func (b *SliderBuilder) WithHtmlId(htmlId string) *SliderBuilder {
	b.check()
	b.htmlId = htmlId
	return b
}

// WithAddress sets the address the slider's values are sent to.
func (b *SliderBuilder) WithAddress(address string) *SliderBuilder {
	b.check()
	b.address = address
	return b
}

// AppendTo puts the slider in the element parentHtmlId (see dom).
func (b *SliderBuilder) AppendTo(parentHtmlId string) *SliderBuilder {
	b.check()
	b.parentHtmlId = parentHtmlId
	return b
}

// Done displays the slider.
func (b *SliderBuilder) Done() *SliderBuilder {
	if b.built {
		panicf("SliderBuilder.Done already called!?")
	}
	b.built = true
	b.track()
	show(b.parentHtmlId, fmt.Sprintf(`<input type="range" id="%s" min="%d" max="%d" value="%d" data-address="%s"/>`,
		html.EscapeString(b.htmlId), b.min, b.max, b.v.Load(), html.EscapeString(b.address)))
	return b
}

// Listen returns a channel receiving the slider's values as it moves.
func (b *SliderBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("SliderBuilder.Listen can only be called after the slider was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the id of the slider's element.
func (b *SliderBuilder) HtmlId() string { return b.htmlId }

// Address returns the address of the slider's values.
func (b *SliderBuilder) Address() string { return b.address }

// Value returns the slider's value.
func (b *SliderBuilder) Value() int { return int(b.v.Load()) }

// SetValue moves the slider.
func (b *SliderBuilder) SetValue(value int) {
	b.v.Store(int64(value))
	comms.Send(b.address, value)
}

// SelectBuilder builds a select: a choice among options.
type SelectBuilder struct {
	value
	htmlId, parentHtmlId string
	options              []string
	built                bool
}

// Select returns a builder for a choice among options. Its value is the
// index of the selected option.
func Select(options []string) *SelectBuilder {
	b := &SelectBuilder{options: options, htmlId: "gonb_select_" + gonbui.UniqueId()}
	b.address = "/select/" + gonbui.UniqueId()
	return b
}

func (b *SelectBuilder) check() {
	if b.built {
		panicf("SelectBuilder cannot change parameters after it is built")
	}
}

// WithHtmlId sets the id of the select's element.
func (b *SelectBuilder) WithHtmlId(htmlId string) *SelectBuilder {
	b.check()
	b.htmlId = htmlId
	return b
}

// WithAddress sets the address the select's values are sent to.
func (b *SelectBuilder) WithAddress(address string) *SelectBuilder {
	b.check()
	b.address = address
	return b
}

// SetDefault selects the option at idx initially.
func (b *SelectBuilder) SetDefault(idx int) *SelectBuilder {
	b.check()
	b.v.Store(int64(idx))
	return b
}

// AppendTo puts the select in the element parentHtmlId (see dom).
func (b *SelectBuilder) AppendTo(parentHtmlId string) *SelectBuilder {
	b.check()
	b.parentHtmlId = parentHtmlId
	return b
}

// Done displays the select.
func (b *SelectBuilder) Done() *SelectBuilder {
	if b.built {
		panicf("SelectBuilder.Done already called!?")
	}
	b.built = true
	b.track()
	var sb strings.Builder
	fmt.Fprintf(&sb, `<select id="%s" data-address="%s">`, html.EscapeString(b.htmlId), html.EscapeString(b.address))
	for i, o := range b.options {
		selected := ""
		if int64(i) == b.v.Load() {
			selected = " selected"
		}
		fmt.Fprintf(&sb, `<option value="%d"%s>%s</option>`, i, selected, html.EscapeString(o))
	}
	sb.WriteString("</select>")
	show(b.parentHtmlId, sb.String())
	return b
}

// Listen returns a channel receiving the index of the selected option at
// each change.
func (b *SelectBuilder) Listen() *comms.AddressChan[int] {
	if !b.built {
		panicf("SelectBuilder.Listen can only be called after the select was created with `Done()` method")
	}
	return comms.Listen[int](b.address)
}

// HtmlId returns the id of the select's element.
func (b *SelectBuilder) HtmlId() string { return b.htmlId }

// Address returns the address of the select's values.
func (b *SelectBuilder) Address() string { return b.address }

// Value returns the index of the selected option.
func (b *SelectBuilder) Value() int { return int(b.v.Load()) }

// SetValue selects the option at index value.
func (b *SelectBuilder) SetValue(value int) {
	b.v.Store(int64(value))
	comms.Send(b.address, value)
}
