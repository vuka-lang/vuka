package live

import (
	"bytes"
	"encoding/json"

	"github.com/vuka-lang/vuka"
)

// The wire protocol: JSON messages over one connection per page (a WebSocket,
// typically), client → server ClientMessage, server → client ServerMessage.
//
//	→ {"type":"join"}
//	← {"type":"render","html":"<main>…</main>"}
//	→ {"type":"event","ref":1,"target":"c2:0","event":"click"}
//	← {"type":"patch","ref":1,"updates":[{"id":"c2","html":"<div data-vk-id=\"c2\">…</div>"}]}
//	→ {"type":"event","ref":2,"target":"c3:1","event":"submit","form":{"title":["Hi"]}}
//	← {"type":"patch","ref":2,"updates":[…],"error":"title is taken"}
//	← {"type":"patch","updates":[…]}                       (a broadcast: Info)
//	← {"type":"error","ref":3,"error":"live: no handler \"c9:0\""}
//
// A client joining with {"type":"join","v":2} gets version 2: render and
// patch carry each instance's render tree, or its change, as Trees
// (components.md, "Protocol v2"):
//
//	→ {"type":"join","v":2}
//	← {"type":"render","v":2,"trees":[{"id":"c0","full":true,"tree":{"t":0,"s":["<main>","","</main>"],"d":[{"id":"c1"}]}},…]}
//	→ {"type":"event","ref":1,"target":"c1:0","event":"click"}
//	← {"type":"patch","ref":1,"trees":[{"id":"c1","tree":{"u":{"2":"1"}}}]}
const (
	Join     = "join"     // client: render the page (the first message)
	EventMsg = "event"    // client: an element's event, to the handler Target names
	Render   = "render"   // server: the page's whole HTML, for the container
	PatchMsg = "patch"    // server: the components that changed, after an event or a broadcast
	Error    = "error"    // server: the message failed; the session goes on
	Redirect = "redirect" // server: reserved, navigate to URL
)

// ClientMessage is a message from the browser. Ref, any number the client
// picks, comes back on the reply.
type ClientMessage struct {
	Type string `json:"type"`
	Ref  int    `json:"ref,omitempty"`
	V    int    `json:"v,omitempty"` // join: the protocol version the client speaks (1 when 0)
	vuka.Event
}

// ServerMessage is a message to the browser.
type ServerMessage struct {
	Type    string       `json:"type"`
	Ref     int          `json:"ref,omitempty"`
	V       int          `json:"v,omitempty"` // render: 2 for a v2 render
	HTML    string       `json:"html,omitempty"`
	Updates []Update     `json:"updates,omitempty"`
	Trees   []TreeUpdate `json:"trees,omitempty"`
	Error   string       `json:"error,omitempty"`
	URL     string       `json:"url,omitempty"`
}

// Handle answers a client message.
func (s *Session) Handle(m ClientMessage) ServerMessage {
	switch m.Type {
	case Join:
		if m.V >= 2 {
			trees, err := s.RenderTrees()
			if err != nil {
				return ServerMessage{Type: Error, Ref: m.Ref, Error: err.Error()}
			}
			return ServerMessage{Type: Render, Ref: m.Ref, V: 2, Trees: trees}
		}
		html, err := s.Render()
		if err != nil {
			return ServerMessage{Type: Error, Ref: m.Ref, Error: err.Error()}
		}
		return ServerMessage{Type: Render, Ref: m.Ref, HTML: html}
	case EventMsg:
		p, err := s.Event(m.Event)
		if err != nil {
			return ServerMessage{Type: Error, Ref: m.Ref, Error: err.Error()}
		}
		return PatchMessage(m.Ref, p)
	}
	return ServerMessage{Type: Error, Ref: m.Ref, Error: "live: unknown message type " + m.Type}
}

// HandleJSON answers a client message's JSON with the reply's.
func (s *Session) HandleJSON(data []byte) []byte {
	var m ClientMessage
	reply := ServerMessage{Type: Error, Error: "live: bad message"}
	if err := json.Unmarshal(data, &m); err == nil {
		reply = s.Handle(m)
	}
	return Marshal(reply)
}

// Marshal is a message's JSON, markup written as markup (not \u003c…).
func Marshal(m ServerMessage) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.Encode(m)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// PatchMessage is the message carrying p, the reply to Ref (0 for a broadcast).
func PatchMessage(ref int, p Patch) ServerMessage {
	return ServerMessage{Type: PatchMsg, Ref: ref, Updates: p.Updates, Trees: p.Trees, Error: p.Error}
}
