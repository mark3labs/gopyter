package notebook

// AddOutput adds an output and returns the outputs and the changed index.
// A nonempty id replaces an existing output in place. Otherwise consecutive
// stdout/stderr chunks without IDs merge, keeping the last 2 MiB past 4 MiB.
func AddOutput(outs []Output, kind OutputKind, text, id string) ([]Output, int) {
	if id != "" {
		for i := range outs {
			if outs[i].ID == id {
				outs[i].Kind, outs[i].Text = kind, text
				return outs, i
			}
		}
	} else if n := len(outs); n > 0 && outs[n-1].ID == "" && outs[n-1].Kind == kind && (kind == Stdout || kind == Stderr) {
		i := n - 1
		outs[i].Text += text
		if len(outs[i].Text) > 4<<20 {
			outs[i].Text = outs[i].Text[len(outs[i].Text)-(2<<20):]
		}
		return outs, i
	}
	outs = append(outs, Output{Kind: kind, Text: text, ID: id})
	return outs, len(outs) - 1
}
