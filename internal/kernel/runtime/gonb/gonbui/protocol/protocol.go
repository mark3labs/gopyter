// Package protocol mirrors GoNB's gonbui/protocol package: the types
// exchanged with the front-end. In gopyter it's part of the GoNB
// compatibility layer; the constants are kept for programs that use them.
//
// Its API and documentation are adapted from GoNB's gonbui/protocol package
// (https://github.com/janpfeifer/gonb/tree/main/gonbui/protocol), by Jan
// Pfeifer, under the MIT license: see LICENSE in the enclosing gonb
// directory.
package protocol

// Environment variables GoNB defines. gopyter defines GONB_DIR and
// GONB_TMP_DIR.
const (
	GONB_PIPE_ENV              = "GONB_PIPE"
	GONB_PIPE_BACK_ENV         = "GONB_PIPE_BACK"
	GONB_DIR_ENV               = "GONB_DIR"
	GONB_TMP_DIR_ENV           = "GONB_TMP_DIR"
	GONB_JUPYTER_ROOT_ENV      = "GONB_JUPYTER_ROOT"
	GONB_JUPYTER_KERNEL_ID_ENV = "GONB_JUPYTER_KERNEL_ID"
	GONB_WASM_DIR_ENV          = "GONB_WASM_DIR"
	GONB_WASM_URL_ENV          = "GONB_WASM_URL"
	GONB_VERSION               = "GONB_VERSION"
	GONB_GIT_COMMIT            = "GONB_GIT_COMMIT"
)

// MIMEType of displayed data.
type MIMEType string

// MIME types of displayed data, and GoNB's own for front-end messages.
const (
	MIMETextHTML       MIMEType = "text/html"
	MIMETextJavascript MIMEType = "text/javascript"
	MIMETextMarkdown   MIMEType = "text/markdown"
	MIMETextPlain      MIMEType = "text/plain"
	MIMEImagePNG       MIMEType = "image/png"
	MIMEImageSVG       MIMEType = "image/svg+xml"
	MIMEJupyterInput   MIMEType = "gonb/jupyter_input"
	MIMECommValue      MIMEType = "gonb/comm_value"
	MIMECommSubscribe  MIMEType = "gonb/comm_subscribe"
)

// DisplayData is data to display, by MIME type. A DisplayID makes later
// data with the same id replace it.
type DisplayData struct {
	Data      map[MIMEType]any
	Metadata  map[string]any
	DisplayID string
}

// InputRequest asks the user to type input for the program's stdin.
type InputRequest struct {
	Prompt   string
	Password bool
}

// CommValueTypes are the value types exchanged with the front-end.
type CommValueTypes interface {
	int | float64 | string | []int | []float64 | []string |
		map[string]int | map[string]float64 | map[string]string
}

// CommValue sends (or, with Request, asks for) the value of an address.
type CommValue struct {
	Address string
	Request bool
	Value   any
}

// CommSubscription (un)subscribes to an address. gopyter sends every
// value to the program, so it's accepted and ignored.
type CommSubscription struct {
	Address     string
	Unsubscribe bool
}

// Internal addresses of GoNB.
const (
	GonbuiSyncAddress    = "#gonbui/sync"
	GonbuiSyncAckAddress = "#gonbui/sync_ack"
	GonbuiStartAddress   = "#comms/start"
)
